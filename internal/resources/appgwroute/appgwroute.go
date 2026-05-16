// Package appgwroute implements the ccp_appgw_route resource — a single
// HTTP route (condition + policies) attached to a `ccp_appgw_listener`.
//
// Routes match on path + headers + methods against the listener's hostname
// and forward to a `ccp_appgw_target_group`. They carry per-route policies:
// rate limit, IP allow/deny, CORS, basic auth, WAF preset, request/response
// header injection.
//
// `header_matches` is a list of nested Block. `basic_auth_users` is a list of
// nested Block with the password field marked Sensitive. The API stores
// these in a Secret Manager-backed reference (`basic_auth_secret_ref`).
//
// `appgw_id` / `listener_id` are immutable; everything else PATCHes in place.
package appgwroute

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/appgwvalidators"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*routeResource)(nil)
	_ resource.ResourceWithConfigure   = (*routeResource)(nil)
	_ resource.ResourceWithImportState = (*routeResource)(nil)
)

func New() resource.Resource { return &routeResource{} }

type routeResource struct{ client *client.Client }

type headerMatchModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
	Op    types.String `tfsdk:"op"`
}

type basicAuthUserModel struct {
	User     types.String `tfsdk:"user"`
	Password types.String `tfsdk:"password"`
}

type routeResourceModel struct {
	ID                 types.String         `tfsdk:"id"`
	AppGWID            types.String         `tfsdk:"appgw_id"`
	ListenerID         types.String         `tfsdk:"listener_id"`
	Priority           types.Int64          `tfsdk:"priority"`
	PathMatch          types.String         `tfsdk:"path_match"`
	PathMatchType      types.String         `tfsdk:"path_match_type"`
	HeaderMatches      []headerMatchModel   `tfsdk:"header_match"`
	MethodMatch        types.List           `tfsdk:"method_match"`
	TargetGroupID      types.String         `tfsdk:"target_group_id"`
	RateLimitPerSec    types.Int64          `tfsdk:"rate_limit_per_sec"`
	AllowCIDRs         types.List           `tfsdk:"allow_cidrs"`
	DenyCIDRs          types.List           `tfsdk:"deny_cidrs"`
	RequestHeaders     types.Map            `tfsdk:"request_headers"`
	ResponseHeaders    types.Map            `tfsdk:"response_headers"`
	CORSEnabled        types.Bool           `tfsdk:"cors_enabled"`
	CORSOrigins        types.List           `tfsdk:"cors_origins"`
	CORSMethods        types.List           `tfsdk:"cors_methods"`
	CORSCredentials    types.Bool           `tfsdk:"cors_credentials"`
	BasicAuthUsers     []basicAuthUserModel `tfsdk:"basic_auth_user"`
	BasicAuthSecretRef types.String         `tfsdk:"basic_auth_secret_ref"`
	WAFPreset          types.String         `tfsdk:"waf_preset"`
}

func (r *routeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_appgw_route"
}

func (r *routeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a single L7 route on a `ccp_appgw_listener`. A route is a `(path + headers + " +
			"methods)` condition plus L7 policies — rate limit, IP allow/deny, CORS, basic auth, WAF preset, request " +
			"and response header injection. The route forwards matched traffic to a `ccp_appgw_target_group`.\n\n" +
			"Routes are evaluated in ascending `priority` order — the first match wins.\n\n" +
			"~> **`basic_auth_user.password` is Sensitive.** Plaintext values are persisted in the Terraform state — " +
			"keep your state backend secure. The platform never returns plaintext back: the server hashes the values " +
			"into a Secret Manager entry (`basic_auth_secret_ref`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the route.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"appgw_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_application_gateway`. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"listener_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the `ccp_appgw_listener` (hostname) this route applies to. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "Evaluation priority (lower = earlier). Defaults to `100`. Two routes with the " +
					"same priority on the same listener have undefined ordering — keep them unique.",
				Optional:   true,
				Computed:   true,
				Default:    int64default.StaticInt64(100),
				Validators: []validator.Int64{int64validator.Between(0, 100000)},
			},
			"path_match": schema.StringAttribute{
				MarkdownDescription: "Path expression to match (e.g. `/api/`, `/v1/users`, `^/[a-z]+/[0-9]+`). " +
					"Interpretation depends on `path_match_type`. Omit to match all paths.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"path_match_type": schema.StringAttribute{
				MarkdownDescription: "How `path_match` is interpreted: `prefix` (default), `exact` or `regex`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("prefix"),
				Validators:          []validator.String{stringvalidator.OneOf(appgwvalidators.PathMatchTypes...)},
			},
			"method_match": schema.ListAttribute{
				MarkdownDescription: "List of HTTP methods to match (e.g. `[\"GET\", \"POST\"]`). Empty list matches all methods.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(appgwvalidators.HTTPMethods...)),
				},
			},
			"target_group_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the `ccp_appgw_target_group` that receives matched traffic.",
				Required:            true,
			},
			"rate_limit_per_sec": schema.Int64Attribute{
				MarkdownDescription: "Per-IP rate limit in req/sec. Null inherits the gateway-wide limit.",
				Optional:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"allow_cidrs": schema.ListAttribute{
				MarkdownDescription: "List of CIDRs allowed to hit this route. Empty list = allow all (subject to " +
					"the gateway-wide `global_allow_cidrs`).",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators:  []validator.List{appgwvalidators.CIDRList()},
			},
			"deny_cidrs": schema.ListAttribute{
				MarkdownDescription: "List of CIDRs denied access to this route. Evaluated before allow.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.List{appgwvalidators.CIDRList()},
			},
			"request_headers": schema.MapAttribute{
				MarkdownDescription: "Headers to set on the request before it reaches the backend (e.g. " +
					"`{\"X-Real-IP\" = \"%[src]\"}`).",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"response_headers": schema.MapAttribute{
				MarkdownDescription: "Headers to set on the response before it leaves the gateway.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
			},
			"cors_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable CORS for this route (sends `Access-Control-Allow-*` headers).",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"cors_origins": schema.ListAttribute{
				MarkdownDescription: "List of origins allowed when `cors_enabled = true` (e.g. " +
					"`[\"https://app.example.com\"]` or `[\"*\"]`).",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"cors_methods": schema.ListAttribute{
				MarkdownDescription: "List of methods allowed when `cors_enabled = true` (defaults to " +
					"`[\"GET\",\"POST\",\"PUT\",\"DELETE\",\"OPTIONS\"]`).",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf(appgwvalidators.HTTPMethods...)),
				},
			},
			"cors_credentials": schema.BoolAttribute{
				MarkdownDescription: "When `true`, sends `Access-Control-Allow-Credentials: true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"basic_auth_secret_ref": schema.StringAttribute{
				MarkdownDescription: "Server-generated reference to the Secret Manager entry storing the hashed " +
					"basic-auth users. Sensitive.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"waf_preset": schema.StringAttribute{
				MarkdownDescription: "WAF preset enforced on this route: `off` (default), `permissive` or `strict`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("off"),
				Validators:          []validator.String{stringvalidator.OneOf(appgwvalidators.WAFPresets...)},
			},
		},
		Blocks: map[string]schema.Block{
			"header_match": schema.ListNestedBlock{
				MarkdownDescription: "Match a request header. Each block adds an AND condition to the route. " +
					"Empty (no blocks) matches any header set.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Header name (case-insensitive).",
							Required:            true,
							Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
						},
						"value": schema.StringAttribute{
							MarkdownDescription: "Expected value (interpretation depends on `op`).",
							Required:            true,
						},
						"op": schema.StringAttribute{
							MarkdownDescription: "Comparison operator: `eq` (default — strict equality), `prefix` " +
								"or `regex`.",
							Optional:   true,
							Computed:   true,
							Default:    stringdefault.StaticString("eq"),
							Validators: []validator.String{stringvalidator.OneOf(appgwvalidators.HeaderOps...)},
						},
					},
				},
			},
			"basic_auth_user": schema.ListNestedBlock{
				MarkdownDescription: "User credential pair for HTTP Basic authentication. Multiple users are " +
					"supported. Declaring at least one block enables basic auth for the route. Omitting the " +
					"block entirely preserves the existing basic auth configuration on update; passing an " +
					"empty list (no blocks) explicitly clears it.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"user": schema.StringAttribute{
							MarkdownDescription: "Username (1-64 chars). Maps to `user` on the API.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.LengthBetween(1, 64)},
						},
						"password": schema.StringAttribute{
							MarkdownDescription: "Plaintext password (1-128 chars) — bcrypt-hashed server-side " +
								"before storage in the encrypted Secret Manager entry referenced by " +
								"`basic_auth_secret_ref`. **Sensitive** and persisted in the Terraform state.",
							Required:   true,
							Sensitive:  true,
							Validators: []validator.String{stringvalidator.LengthBetween(1, 128)},
						},
					},
				},
			},
		},
	}
}

func (r *routeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Got %T", req.ProviderData))
		return
	}
	r.client = c
}

// ─── State <-> API mapping ──────────────────────────────────────────────────

func stringsFromList(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(l.Elements()))
	l.ElementsAs(ctx, &out, false)
	return out
}

func stringMapFromMap(ctx context.Context, m types.Map) map[string]string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := make(map[string]string, len(m.Elements()))
	m.ElementsAs(ctx, &out, false)
	return out
}

func apiHeaderMatches(in []headerMatchModel) []client.AppGWHeaderMatch {
	if len(in) == 0 {
		return nil
	}
	out := make([]client.AppGWHeaderMatch, 0, len(in))
	for _, h := range in {
		op := "eq"
		if !h.Op.IsNull() && !h.Op.IsUnknown() && h.Op.ValueString() != "" {
			op = h.Op.ValueString()
		}
		out = append(out, client.AppGWHeaderMatch{
			Name:  h.Name.ValueString(),
			Value: h.Value.ValueString(),
			Op:    op,
		})
	}
	return out
}

func apiBasicAuthUsers(in []basicAuthUserModel) []client.AppGWBasicAuthUser {
	if len(in) == 0 {
		return nil
	}
	out := make([]client.AppGWBasicAuthUser, 0, len(in))
	for _, u := range in {
		out = append(out, client.AppGWBasicAuthUser{
			User:     u.User.ValueString(),
			Password: u.Password.ValueString(),
		})
	}
	return out
}

// applyToModel maps the API route back onto the model. IMPORTANT — does
// NOT touch BasicAuthUsers: the API never returns plaintext passwords,
// so the caller must preserve the plan-side list before invoking this
// (mirrors the secret.applySecretToModel pattern).
func applyToModel(ctx context.Context, rt *client.AppGWRoute, m *routeResourceModel) {
	m.ID = types.StringValue(rt.ID)
	m.AppGWID = types.StringValue(rt.AppGWID)
	m.ListenerID = types.StringValue(rt.ListenerID)
	m.Priority = types.Int64Value(rt.Priority)
	if rt.PathMatch != nil {
		m.PathMatch = types.StringValue(*rt.PathMatch)
	} else {
		m.PathMatch = types.StringNull()
	}
	m.PathMatchType = types.StringValue(rt.PathMatchType)
	m.TargetGroupID = types.StringValue(rt.TargetGroupID)
	m.WAFPreset = types.StringValue(rt.WAFPreset)
	m.CORSEnabled = types.BoolValue(rt.CORSEnabled)
	m.CORSCredentials = types.BoolValue(rt.CORSCredentials)
	if rt.RateLimitPerSec != nil {
		m.RateLimitPerSec = types.Int64Value(*rt.RateLimitPerSec)
	} else {
		m.RateLimitPerSec = types.Int64Null()
	}
	if rt.BasicAuthSecretRef != nil {
		m.BasicAuthSecretRef = types.StringValue(*rt.BasicAuthSecretRef)
	} else {
		m.BasicAuthSecretRef = types.StringNull()
	}

	// header_matches
	hmodels := make([]headerMatchModel, 0, len(rt.HeaderMatches))
	for _, h := range rt.HeaderMatches {
		op := h.Op
		if op == "" {
			op = "eq"
		}
		hmodels = append(hmodels, headerMatchModel{
			Name:  types.StringValue(h.Name),
			Value: types.StringValue(h.Value),
			Op:    types.StringValue(op),
		})
	}
	m.HeaderMatches = hmodels

	// list fields
	methods, _ := types.ListValueFrom(ctx, types.StringType, rt.MethodMatch)
	m.MethodMatch = methods
	allow, _ := types.ListValueFrom(ctx, types.StringType, rt.AllowCIDRs)
	m.AllowCIDRs = allow
	deny, _ := types.ListValueFrom(ctx, types.StringType, rt.DenyCIDRs)
	m.DenyCIDRs = deny
	corsOrigins, _ := types.ListValueFrom(ctx, types.StringType, rt.CORSOrigins)
	m.CORSOrigins = corsOrigins
	corsMethods, _ := types.ListValueFrom(ctx, types.StringType, rt.CORSMethods)
	m.CORSMethods = corsMethods

	// maps
	reqHeaders, _ := types.MapValueFrom(ctx, types.StringType, rt.RequestHeaders)
	m.RequestHeaders = reqHeaders
	respHeaders, _ := types.MapValueFrom(ctx, types.StringType, rt.ResponseHeaders)
	m.ResponseHeaders = respHeaders
}

// ─── CRUD ──────────────────────────────────────────────────────────────────

func (r *routeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.AppGWRouteCreateRequest{
		ListenerID:    plan.ListenerID.ValueString(),
		TargetGroupID: plan.TargetGroupID.ValueString(),
		HeaderMatches: apiHeaderMatches(plan.HeaderMatches),
		BasicAuthUsers: apiBasicAuthUsers(plan.BasicAuthUsers),
	}
	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		v := plan.Priority.ValueInt64()
		createReq.Priority = &v
	}
	if !plan.PathMatch.IsNull() && !plan.PathMatch.IsUnknown() && plan.PathMatch.ValueString() != "" {
		v := plan.PathMatch.ValueString()
		createReq.PathMatch = &v
	}
	if !plan.PathMatchType.IsNull() && !plan.PathMatchType.IsUnknown() {
		v := plan.PathMatchType.ValueString()
		createReq.PathMatchType = &v
	}
	if !plan.RateLimitPerSec.IsNull() && !plan.RateLimitPerSec.IsUnknown() {
		v := plan.RateLimitPerSec.ValueInt64()
		createReq.RateLimitPerSec = &v
	}
	if !plan.CORSEnabled.IsNull() && !plan.CORSEnabled.IsUnknown() {
		v := plan.CORSEnabled.ValueBool()
		createReq.CORSEnabled = &v
	}
	if !plan.CORSCredentials.IsNull() && !plan.CORSCredentials.IsUnknown() {
		v := plan.CORSCredentials.ValueBool()
		createReq.CORSCredentials = &v
	}
	if !plan.WAFPreset.IsNull() && !plan.WAFPreset.IsUnknown() {
		v := plan.WAFPreset.ValueString()
		createReq.WAFPreset = &v
	}
	createReq.MethodMatch = stringsFromList(ctx, plan.MethodMatch)
	createReq.AllowCIDRs = stringsFromList(ctx, plan.AllowCIDRs)
	createReq.DenyCIDRs = stringsFromList(ctx, plan.DenyCIDRs)
	createReq.CORSOrigins = stringsFromList(ctx, plan.CORSOrigins)
	createReq.CORSMethods = stringsFromList(ctx, plan.CORSMethods)
	createReq.RequestHeaders = stringMapFromMap(ctx, plan.RequestHeaders)
	createReq.ResponseHeaders = stringMapFromMap(ctx, plan.ResponseHeaders)

	created, err := r.client.CreateAppGWRoute(ctx, plan.AppGWID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AppGW route", err.Error())
		return
	}

	// Preserve plan-side basic_auth_user list — the API does not echo
	// plaintext passwords back. Same idiom as ccp_secret.data.
	users := plan.BasicAuthUsers
	applyToModel(ctx, created, &plan)
	plan.BasicAuthUsers = users
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *routeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetAppGWRoute(ctx, state.AppGWID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read AppGW route", err.Error())
		return
	}
	users := state.BasicAuthUsers
	applyToModel(ctx, got, &state)
	state.BasicAuthUsers = users
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *routeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state routeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var upd client.AppGWRouteUpdateRequest
	if !plan.Priority.Equal(state.Priority) {
		v := plan.Priority.ValueInt64()
		upd.Priority = &v
	}
	if !plan.PathMatch.Equal(state.PathMatch) {
		if plan.PathMatch.IsNull() {
			empty := ""
			upd.PathMatch = &empty
		} else {
			v := plan.PathMatch.ValueString()
			upd.PathMatch = &v
		}
	}
	if !plan.PathMatchType.Equal(state.PathMatchType) {
		v := plan.PathMatchType.ValueString()
		upd.PathMatchType = &v
	}
	if !plan.TargetGroupID.Equal(state.TargetGroupID) {
		v := plan.TargetGroupID.ValueString()
		upd.TargetGroupID = &v
	}
	if !plan.RateLimitPerSec.Equal(state.RateLimitPerSec) {
		if plan.RateLimitPerSec.IsNull() {
			zero := int64(0)
			upd.RateLimitPerSec = &zero
		} else {
			v := plan.RateLimitPerSec.ValueInt64()
			upd.RateLimitPerSec = &v
		}
	}
	if !plan.WAFPreset.Equal(state.WAFPreset) {
		v := plan.WAFPreset.ValueString()
		upd.WAFPreset = &v
	}
	if !plan.CORSEnabled.Equal(state.CORSEnabled) {
		v := plan.CORSEnabled.ValueBool()
		upd.CORSEnabled = &v
	}
	if !plan.CORSCredentials.Equal(state.CORSCredentials) {
		v := plan.CORSCredentials.ValueBool()
		upd.CORSCredentials = &v
	}

	// Reset lists / maps unconditionally if they changed (replace
	// semantics; the API treats `[]` and `{}` as "clear").
	if !plan.MethodMatch.Equal(state.MethodMatch) {
		v := stringsFromList(ctx, plan.MethodMatch)
		if v == nil {
			v = []string{}
		}
		upd.MethodMatch = &v
	}
	if !plan.AllowCIDRs.Equal(state.AllowCIDRs) {
		v := stringsFromList(ctx, plan.AllowCIDRs)
		if v == nil {
			v = []string{}
		}
		upd.AllowCIDRs = &v
	}
	if !plan.DenyCIDRs.Equal(state.DenyCIDRs) {
		v := stringsFromList(ctx, plan.DenyCIDRs)
		if v == nil {
			v = []string{}
		}
		upd.DenyCIDRs = &v
	}
	if !plan.CORSOrigins.Equal(state.CORSOrigins) {
		v := stringsFromList(ctx, plan.CORSOrigins)
		if v == nil {
			v = []string{}
		}
		upd.CORSOrigins = &v
	}
	if !plan.CORSMethods.Equal(state.CORSMethods) {
		v := stringsFromList(ctx, plan.CORSMethods)
		if v == nil {
			v = []string{}
		}
		upd.CORSMethods = &v
	}
	if !plan.RequestHeaders.Equal(state.RequestHeaders) {
		v := stringMapFromMap(ctx, plan.RequestHeaders)
		if v == nil {
			v = map[string]string{}
		}
		upd.RequestHeaders = &v
	}
	if !plan.ResponseHeaders.Equal(state.ResponseHeaders) {
		v := stringMapFromMap(ctx, plan.ResponseHeaders)
		if v == nil {
			v = map[string]string{}
		}
		upd.ResponseHeaders = &v
	}

	// header_match / basic_auth_user — always-set if any diff. We compare
	// length + element values directly since slice equality via the
	// framework's `Equal()` isn't free on nested models.
	if headerMatchesChanged(plan.HeaderMatches, state.HeaderMatches) {
		v := apiHeaderMatches(plan.HeaderMatches)
		if v == nil {
			v = []client.AppGWHeaderMatch{}
		}
		upd.HeaderMatches = &v
	}
	if basicAuthChanged(plan.BasicAuthUsers, state.BasicAuthUsers) {
		v := apiBasicAuthUsers(plan.BasicAuthUsers)
		if v == nil {
			v = []client.AppGWBasicAuthUser{}
		}
		upd.BasicAuthUsers = &v
	}

	got, err := r.client.UpdateAppGWRoute(ctx, state.AppGWID.ValueString(), state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update AppGW route", err.Error())
		return
	}
	users := plan.BasicAuthUsers
	applyToModel(ctx, got, &plan)
	plan.BasicAuthUsers = users
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func headerMatchesChanged(a, b []headerMatchModel) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if !a[i].Name.Equal(b[i].Name) || !a[i].Value.Equal(b[i].Value) || !a[i].Op.Equal(b[i].Op) {
			return true
		}
	}
	return false
}

func basicAuthChanged(a, b []basicAuthUserModel) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if !a[i].User.Equal(b[i].User) || !a[i].Password.Equal(b[i].Password) {
			return true
		}
	}
	return false
}

func (r *routeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAppGWRoute(ctx, state.AppGWID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete AppGW route", err.Error())
	}
}

func (r *routeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<appgw_id>/<route_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("appgw_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	// Leave basic_auth_user empty after import — the API never returns plaintext.
}
