package objectbucket

import (
	"context"
	"fmt"
	"sort"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

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
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"allowed_origins": schema.SetAttribute{
					MarkdownDescription: "Origins allowed to call the bucket, e.g. " +
						"`https://app.example.com`. A single `*` subdomain wildcard " +
						"(`https://*.example.com`) may be accepted depending on " +
						"platform settings; a bare `*` is only valid on a public bucket.",
					Required:    true,
					ElementType: types.StringType,
					Validators:  []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))},
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
				},
				"expose_headers": schema.SetAttribute{
					MarkdownDescription: "Response headers exposed to the page, e.g. `ETag`.",
					Optional:            true,
					Computed:            true,
					ElementType:         types.StringType,
				},
				"max_age_seconds": schema.Int64Attribute{
					MarkdownDescription: "How long (seconds) the browser may cache the preflight answer. Defaults to `3600`.",
					Optional:            true,
					Computed:            true,
					Default:             int64default.StaticInt64(3600),
					Validators:          []validator.Int64{int64validator.AtLeast(0)},
				},
				"description": schema.StringAttribute{
					MarkdownDescription: "Free-form label for the rule.",
					Optional:            true,
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

// writeCors makes the bucket's rules equal to `rules`. The revision is read just
// before the write so that a change made elsewhere between the two is refused
// by the API (409) instead of silently overwritten.
func (r *objectBucketResource) writeCors(ctx context.Context, id string, rules []client.BucketCorsRule) error {
	cur, err := r.client.GetBucketCors(ctx, id)
	if err != nil {
		return fmt.Errorf("read current CORS rules: %w", err)
	}
	if len(rules) == 0 {
		if cur.Status == "none" {
			return nil
		}
		return r.client.DeleteBucketCors(ctx, id, cur.Revision)
	}
	_, err = r.client.PutBucketCors(ctx, id, client.BucketCorsUpdateRequest{
		Rules: rules, ExpectedRevision: cur.Revision,
	})
	return err
}
