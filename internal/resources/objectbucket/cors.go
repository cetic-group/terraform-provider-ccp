package objectbucket

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Limits mirrored from the platform so that a refused rule is reported at plan
// time (validate / plan) instead of at apply. The platform can be stricter
// (operator settings: number of rules, methods, wildcards, localhost); those
// refusals still surface at apply time.
const (
	corsMaxRules       = 100
	corsMaxPerRule     = 20
	corsMaxOriginLen   = 255
	corsMaxHeaderLen   = 128
	corsMaxDescription = 200
	corsMaxAge         = 86400
)

// A header name: token characters; `*` is only accepted as the last character
// (`*` alone, or a prefix such as `x-amz-meta-*`).
var corsHeaderPattern = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_` + "`" + `|~-]+$`)

// canonicalCorsOrigin returns the form the platform stores for an origin, or an
// error when the value is not an origin. An origin that is not already in this
// form would come back different from the configuration and cause a permanent
// diff, so the validator refuses it and names the expected form.
func canonicalCorsOrigin(v string) (string, error) {
	if v == "*" {
		return v, nil
	}
	if v == "" || len(v) > corsMaxOriginLen {
		return "", fmt.Errorf("must be 1–%d characters", corsMaxOriginLen)
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return "", fmt.Errorf("must look like https://host[:port]")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("must not contain a path, query, fragment or credentials")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	out := strings.ToLower(u.Scheme) + "://" + host
	if port != "" {
		out += ":" + port
	}
	return out, nil
}

type corsOriginValidator struct{}

func (corsOriginValidator) Description(context.Context) string {
	return "origin in canonical form: lowercase scheme and host, no path, no trailing slash, no default port"
}
func (v corsOriginValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (corsOriginValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	want, err := canonicalCorsOrigin(got)
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CORS origin", fmt.Sprintf("%q: %s.", got, err))
		return
	}
	if want != got {
		resp.Diagnostics.AddAttributeError(req.Path, "CORS origin is not in canonical form",
			fmt.Sprintf("%q is stored as %q. Write it that way to avoid a permanent difference "+
				"(lowercase scheme and host, no trailing slash, no default port).", got, want))
	}
}

type corsHeaderValidator struct{}

func (corsHeaderValidator) Description(context.Context) string {
	return "valid header name; `*` only as the last character"
}
func (v corsHeaderValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (corsHeaderValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	h := req.ConfigValue.ValueString()
	switch {
	case len(h) == 0 || len(h) > corsMaxHeaderLen:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CORS header", fmt.Sprintf("%q must be 1–%d characters.", h, corsMaxHeaderLen))
	case !corsHeaderPattern.MatchString(h):
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CORS header", fmt.Sprintf("%q is not a valid header name.", h))
	case strings.Contains(h, "*") && !(strings.HasSuffix(h, "*") && strings.Count(h, "*") == 1):
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CORS header",
			fmt.Sprintf("%q: `*` is only accepted at the end of a header (for example `x-amz-meta-*`).", h))
	}
}

// corsNoCaseDuplicates refuses a set holding the same header twice with a
// different case: the platform keeps one, which would be a permanent difference.
type corsNoCaseDuplicates struct{}

func (corsNoCaseDuplicates) Description(context.Context) string {
	return "no two values that differ only by case"
}
func (v corsNoCaseDuplicates) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (corsNoCaseDuplicates) ValidateSet(ctx context.Context, req validator.SetRequest, resp *validator.SetResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var vals []string
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &vals, false)...)
	seen := map[string]string{}
	for _, v := range vals {
		k := strings.ToLower(v)
		if prev, ok := seen[k]; ok {
			resp.Diagnostics.AddAttributeError(req.Path, "Duplicate CORS header",
				fmt.Sprintf("%q and %q only differ by case; keep one.", prev, v))
			return
		}
		seen[k] = v
	}
}

// corsRuleAttrTypes is the object type of one `cors_rule` block.
var corsRuleAttrTypes = map[string]attr.Type{
	"allowed_origins": types.SetType{ElemType: types.StringType},
	"allowed_methods": types.SetType{ElemType: types.StringType},
	"allowed_headers": types.SetType{ElemType: types.StringType},
	"expose_headers":  types.SetType{ElemType: types.StringType},
	"max_age_seconds": types.Int64Type,
	"description":     types.StringType,
}

// corsRuleBlock is the schema of the repeatable `cors_rule` block.
//
// Rules are an ordered list: the order is kept as written. When no block is
// configured the provider does NOT manage CORS and leaves any existing
// configuration (console, CLI) untouched; removing every block of a bucket
// that had some deletes them.
func corsRuleBlock() schema.ListNestedBlock {
	return schema.ListNestedBlock{
		MarkdownDescription: "A CORS rule: which web origins may call the bucket " +
			"directly from a browser, with which methods and headers. Repeat the " +
			"block for several rules. Without any `cors_rule`, CORS is not managed " +
			"by this resource and an existing configuration is left untouched. " +
			"Platform limits (number of rules, allowed methods, `localhost`, " +
			"wildcards) apply and are reported at apply time.",
		Validators: []validator.List{listvalidator.SizeAtMost(corsMaxRules)},
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"allowed_origins": schema.SetAttribute{
					MarkdownDescription: "Origins allowed to call the bucket, e.g. " +
						"`https://app.example.com`. A single `*` subdomain wildcard " +
						"(`https://*.example.com`) may be accepted depending on " +
						"platform settings; a bare `*` is only valid on a public bucket.",
					Required:    true,
					ElementType: types.StringType,
					Validators: []validator.Set{
						setvalidator.SizeBetween(1, corsMaxPerRule),
						setvalidator.ValueStringsAre(corsOriginValidator{}),
					},
				},
				"allowed_methods": schema.SetAttribute{
					MarkdownDescription: "HTTP methods allowed: `GET`, `PUT`, `POST`, `DELETE`, `HEAD`.",
					Required:            true,
					ElementType:         types.StringType,
					Validators: []validator.Set{
						setvalidator.SizeAtLeast(1),
						setvalidator.ValueStringsAre(stringvalidator.OneOf("GET", "PUT", "POST", "DELETE", "HEAD")),
					},
				},
				"allowed_headers": schema.SetAttribute{
					MarkdownDescription: "Request headers the browser may send, e.g. `Content-Type`. " +
						"Prefer an explicit list over `*`.",
					Optional:    true,
					Computed:    true,
					ElementType: types.StringType,
					// Empty by default: an unset set must never stay unknown after apply.
					Default: setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
					Validators: []validator.Set{
						setvalidator.SizeAtMost(corsMaxPerRule),
						setvalidator.ValueStringsAre(corsHeaderValidator{}),
						corsNoCaseDuplicates{},
					},
				},
				"expose_headers": schema.SetAttribute{
					MarkdownDescription: "Response headers exposed to the page, e.g. `ETag`.",
					Optional:            true,
					Computed:            true,
					ElementType:         types.StringType,
					Default:             setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
					Validators: []validator.Set{
						setvalidator.SizeAtMost(corsMaxPerRule),
						setvalidator.ValueStringsAre(corsHeaderValidator{}),
						corsNoCaseDuplicates{},
					},
				},
				"max_age_seconds": schema.Int64Attribute{
					MarkdownDescription: "How long (seconds) the browser may cache the preflight answer. Defaults to `3600`.",
					Optional:            true,
					Computed:            true,
					Default:             int64default.StaticInt64(3600),
					Validators:          []validator.Int64{int64validator.Between(0, corsMaxAge)},
				},
				"description": schema.StringAttribute{
					MarkdownDescription: "Free-form label for the rule (1–200 characters, no leading or trailing space).",
					Optional:            true,
					Validators: []validator.String{
						stringvalidator.LengthBetween(1, corsMaxDescription),
						stringvalidator.RegexMatches(regexp.MustCompile(`^\S(.*\S)?$`), "must not start or end with whitespace"),
					},
				},
			},
		},
	}
}

// corsRuleModel mirrors one `cors_rule` block.
type corsRuleModel struct {
	AllowedOrigins types.Set    `tfsdk:"allowed_origins"`
	AllowedMethods types.Set    `tfsdk:"allowed_methods"`
	AllowedHeaders types.Set    `tfsdk:"allowed_headers"`
	ExposeHeaders  types.Set    `tfsdk:"expose_headers"`
	MaxAgeSeconds  types.Int64  `tfsdk:"max_age_seconds"`
	Description    types.String `tfsdk:"description"`
}

func setToStrings(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	out := []string{}
	if s.IsNull() || s.IsUnknown() {
		return out, nil
	}
	diags := s.ElementsAs(ctx, &out, false)
	sort.Strings(out)
	return out, diags
}

func stringsToSet(ctx context.Context, v []string) (types.Set, diag.Diagnostics) {
	if v == nil {
		v = []string{}
	}
	return types.SetValueFrom(ctx, types.StringType, v)
}

// corsRulesFromPlan converts the planned blocks to API rules.
func corsRulesFromPlan(ctx context.Context, list types.List) ([]client.BucketCorsRule, diag.Diagnostics) {
	var diags diag.Diagnostics
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	var models []corsRuleModel
	diags.Append(list.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return nil, diags
	}
	out := make([]client.BucketCorsRule, 0, len(models))
	for _, m := range models {
		var r client.BucketCorsRule
		var d diag.Diagnostics
		r.AllowedOrigins, d = setToStrings(ctx, m.AllowedOrigins)
		diags.Append(d...)
		r.AllowedMethods, d = setToStrings(ctx, m.AllowedMethods)
		diags.Append(d...)
		r.AllowedHeaders, d = setToStrings(ctx, m.AllowedHeaders)
		diags.Append(d...)
		r.ExposeHeaders, d = setToStrings(ctx, m.ExposeHeaders)
		diags.Append(d...)
		r.MaxAgeSeconds = 3600
		if !m.MaxAgeSeconds.IsNull() && !m.MaxAgeSeconds.IsUnknown() {
			r.MaxAgeSeconds = m.MaxAgeSeconds.ValueInt64()
		}
		if !m.Description.IsNull() && !m.Description.IsUnknown() {
			v := m.Description.ValueString()
			r.Description = &v
		}
		out = append(out, r)
	}
	return out, diags
}

// corsRulesToState converts API rules to the state representation.
func corsRulesToState(ctx context.Context, rules []client.BucketCorsRule) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	objType := types.ObjectType{AttrTypes: corsRuleAttrTypes}
	elems := make([]attr.Value, 0, len(rules))
	for _, r := range rules {
		o, d := stringsToSet(ctx, r.AllowedOrigins)
		diags.Append(d...)
		m, d := stringsToSet(ctx, r.AllowedMethods)
		diags.Append(d...)
		h, d := stringsToSet(ctx, r.AllowedHeaders)
		diags.Append(d...)
		e, d := stringsToSet(ctx, r.ExposeHeaders)
		diags.Append(d...)
		desc := types.StringNull()
		if r.Description != nil {
			desc = types.StringValue(*r.Description)
		}
		obj, d := types.ObjectValue(corsRuleAttrTypes, map[string]attr.Value{
			"allowed_origins": o, "allowed_methods": m, "allowed_headers": h,
			"expose_headers": e, "max_age_seconds": types.Int64Value(r.MaxAgeSeconds),
			"description": desc,
		})
		diags.Append(d...)
		elems = append(elems, obj)
	}
	if diags.HasError() {
		return types.ListNull(objType), diags
	}
	l, d := types.ListValue(objType, elems)
	diags.Append(d...)
	return l, diags
}

func corsRuleCount(l types.List) int {
	if l.IsNull() || l.IsUnknown() {
		return 0
	}
	return len(l.Elements())
}

// writeCors makes the bucket's rules equal to `rules` and returns the new
// revision ("" when the rules were deleted).
//
// `expected` is the revision recorded in the state at the last refresh: the
// platform refuses the write (409) if the rules changed since, so a change made
// by someone else between `plan` and `apply` is never overwritten. When it is
// empty (first write, or a state without revision) the current revision is read
// just before the write, which only protects the instants in between.
//
// An empty `rules` deletes: callers must only ask for it when the practitioner
// had rules in the previous state (never to "clean" a bucket they do not manage).
func (r *objectBucketResource) writeCors(ctx context.Context, id string, rules []client.BucketCorsRule, expected string) (string, error) {
	if expected == "" {
		cur, err := r.client.GetBucketCors(ctx, id)
		if err != nil {
			return "", fmt.Errorf("read current CORS rules: %w", err)
		}
		if len(rules) == 0 && cur.Status == "none" {
			return "", nil
		}
		expected = cur.Revision
	}
	if len(rules) == 0 {
		return "", r.client.DeleteBucketCors(ctx, id, expected)
	}
	out, err := r.client.PutBucketCors(ctx, id, client.BucketCorsUpdateRequest{
		Rules: rules, ExpectedRevision: expected,
	})
	if err != nil {
		return "", err
	}
	return out.Revision, nil
}

// revisionValue turns a revision into the state value: null when empty.
func revisionValue(rev string) types.String {
	if rev == "" {
		return types.StringNull()
	}
	return types.StringValue(rev)
}

// corsRevisionAttribute holds the revision of the CORS rules as of the last read
// or write. It is sent back with the next write so that a change made elsewhere
// in the meantime is refused instead of overwritten. Null while no `cors_rule`
// is managed.
func corsRevisionAttribute() schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: "Revision of the CORS rules as of the last refresh or write. Sent with the next " +
			"write so that a change made elsewhere in the meantime is refused instead of overwritten. " +
			"Null when no `cors_rule` block is managed.",
		Computed: true,
	}
}

// emptyCorsRules is the "no rule" value of the `cors_rule` block list.
func emptyCorsRules() types.List {
	return types.ListValueMust(types.ObjectType{AttrTypes: corsRuleAttrTypes}, []attr.Value{})
}

// applyCorsOnCreate writes the planned rules to a freshly created bucket.
//
// ⚠️ A failure must NOT be returned as an error: Terraform would mark the whole
// resource tainted and the next apply would destroy and recreate the bucket,
// objects included, instead of letting the practitioner fix the rule. The bucket
// exists and is fine; the state records the truth (no rule applied), a warning
// explains why, and the next plan shows the rules still to apply.
func (r *objectBucketResource) applyCorsOnCreate(ctx context.Context, id string, plan *objectBucketResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	// A computed attribute must be known after apply, whatever happens below.
	plan.CorsRevision = types.StringNull()
	if corsRuleCount(plan.CorsRule) == 0 {
		return diags
	}
	rules, d := corsRulesFromPlan(ctx, plan.CorsRule)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	rev, err := r.writeCors(ctx, id, rules, "")
	if err == nil {
		plan.CorsRevision = revisionValue(rev)
	}
	if err != nil {
		plan.CorsRule = emptyCorsRules()
		plan.CorsRevision = types.StringNull()
		diags.AddWarning("CORS rules were not applied",
			fmt.Sprintf("Bucket %s was created, but the platform refused its CORS rules: %s\n\n"+
				"The bucket is kept (it is not replaced). Fix the `cors_rule` block and run `terraform apply` "+
				"again: the rules are still planned.", id, err.Error()))
	}
	return diags
}

// corsStatusWarning reports rules that are saved but not applied to the bucket.
func corsStatusWarning(id string, c *client.BucketCors) (string, string, bool) {
	switch c.Status {
	case "error":
		msg := "the platform could not apply them"
		if c.ErrorMessage != nil && *c.ErrorMessage != "" {
			msg = *c.ErrorMessage
		}
		return "CORS rules are saved but not applied",
			fmt.Sprintf("Bucket %s: %s. The platform retries automatically; until then browsers are not "+
				"served the configured rules.", id, msg), true
	case "pending":
		return "CORS rules are still being applied",
			fmt.Sprintf("Bucket %s: the rules are saved and not yet applied to the bucket.", id), true
	}
	return "", "", false
}

// ValidateConfig checks what depends on several attributes at once: a bare `*`
// origin is only valid on a public bucket, alone in its rule and without a
// method that writes.
func (r *objectBucketResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg objectBucketResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.CorsRule.IsNull() || cfg.CorsRule.IsUnknown() {
		return
	}
	var rules []corsRuleModel
	if d := cfg.CorsRule.ElementsAs(ctx, &rules, false); d.HasError() {
		return // unknown values (variables): judged at plan/apply
	}
	for _, rule := range rules {
		if rule.AllowedOrigins.IsNull() || rule.AllowedOrigins.IsUnknown() {
			continue
		}
		origins, _ := setToStrings(ctx, rule.AllowedOrigins)
		wildcard := false
		for _, o := range origins {
			if o == "*" {
				wildcard = true
			}
		}
		if !wildcard {
			continue
		}
		p := path.Root("cors_rule")
		if len(origins) > 1 {
			resp.Diagnostics.AddAttributeError(p, "Invalid CORS origin", "`*` cannot be combined with other origins in the same rule.")
		}
		if !cfg.IsPublic.IsUnknown() && !cfg.IsPublic.ValueBool() {
			resp.Diagnostics.AddAttributeError(p, "Invalid CORS origin", "The origin `*` is only accepted on a public bucket (`is_public = true`).")
		}
		methods, _ := setToStrings(ctx, rule.AllowedMethods)
		for _, m := range methods {
			if m == "PUT" || m == "POST" || m == "DELETE" {
				resp.Diagnostics.AddAttributeError(p, "Invalid CORS origin", "The origin `*` cannot be used with a method that writes (PUT, POST, DELETE).")
				break
			}
		}
	}
}
