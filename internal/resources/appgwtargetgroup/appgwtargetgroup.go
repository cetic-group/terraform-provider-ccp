// Package appgwtargetgroup implements the ccp_appgw_target_group
// Terraform resource — either a pool of backends with load-balancing algorithm
// and health-check configuration (`kind = "upstream"`), or a public bucket
// served directly by the gateway (`kind = "bucket"`), used by routes via
// `target_group_id`.
//
// Members (the actual backends) are managed via ccp_appgw_target_group_member.
package appgwtargetgroup

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/appgwvalidators"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	ccppm "github.com/cetic-group/terraform-provider-ccp/internal/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                   = (*tgResource)(nil)
	_ resource.ResourceWithConfigure      = (*tgResource)(nil)
	_ resource.ResourceWithImportState    = (*tgResource)(nil)
	_ resource.ResourceWithValidateConfig = (*tgResource)(nil)
)

const (
	kindUpstream = "upstream"
	kindBucket   = "bucket"
)

func New() resource.Resource { return &tgResource{} }

type tgResource struct{ client *client.Client }

type tgResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	AppGWID              types.String `tfsdk:"appgw_id"`
	Name                 types.String `tfsdk:"name"`
	Algorithm            types.String `tfsdk:"algorithm"`
	HCProtocol           types.String `tfsdk:"hc_protocol"`
	HCMethod             types.String `tfsdk:"hc_method"`
	HCPath               types.String `tfsdk:"hc_path"`
	HCExpectStatus       types.Int64  `tfsdk:"hc_expect_status"`
	HCIntervalSec        types.Int64  `tfsdk:"hc_interval_sec"`
	HCTimeoutSec         types.Int64  `tfsdk:"hc_timeout_sec"`
	HCHealthyThreshold   types.Int64  `tfsdk:"hc_healthy_threshold"`
	HCUnhealthyThreshold types.Int64  `tfsdk:"hc_unhealthy_threshold"`
	StickyEnabled        types.Bool   `tfsdk:"sticky_enabled"`
	StickyCookieName     types.String `tfsdk:"sticky_cookie_name"`
	Kind                 types.String `tfsdk:"kind"`
	BucketID             types.String `tfsdk:"bucket_id"`
	BucketPrefix         types.String `tfsdk:"bucket_prefix"`
	OriginStatus         types.String `tfsdk:"origin_status"`
	OriginError          types.String `tfsdk:"origin_error"`
}

func (r *tgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_appgw_target_group"
}

func (r *tgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a target group on a `ccp_application_gateway` — a pool of backends with " +
			"load-balancing algorithm and L7 health-check configuration. Routes reference target groups via " +
			"`target_group_id`. Members are managed via `ccp_appgw_target_group_member`.\n\n" +
			"A target group is either a pool of servers (`kind = \"upstream\"`, the default) or a **bucket target** " +
			"(`kind = \"bucket\"`): the gateway then serves a public bucket, or one folder of it, under your own domain, " +
			"read-only (`GET` and `HEAD`, no listing of the content). A bucket target has no members, health check, " +
			"algorithm or session persistence: those arguments are rejected. Cache behaviour is set on the route " +
			"(`ccp_appgw_route`), not on the target group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the target group.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"appgw_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_application_gateway`. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Target group name, unique per gateway (1-100 chars).",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "Target group type: `upstream` (default, a pool of servers) or `bucket` (a public bucket " +
					"served by the gateway; requires `bucket_id`). **Immutable** — changing it replaces the target group. " +
					"With `bucket`, the arguments of a pool of servers (`algorithm`, `hc_*`, `sticky_*`) are rejected even when " +
					"set to their default value: leave them out. This check is made at plan time and is skipped while " +
					"`kind` is not yet known (for example when it comes from another resource); the platform then " +
					"refuses an invalid combination at apply time.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{stringvalidator.OneOf(kindUpstream, kindBucket)},
				PlanModifiers: []planmodifier.String{
					ccppm.LegacyStringDefault(kindUpstream),
					ccppm.RequiresReplaceUnlessLegacy(kindUpstream),
				},
			},
			"bucket_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the bucket to serve. Required with `kind = \"bucket\"`, not allowed otherwise. " +
					"The bucket must be public, active, and in the same region as the gateway. Can be changed in place " +
					"to serve another bucket.",
				Optional:   true,
				Validators: []validator.String{canonicalUUIDValidator{}},
			},
			"bucket_prefix": schema.StringAttribute{
				MarkdownDescription: "Folder of the bucket to serve, for example `site/assets` (default: the whole bucket). " +
					"Letters, digits, `.`, `_`, `-` and `/` only, at most 512 characters, no `.` or `..` segment, " +
					"no leading or trailing `/`. Only with `kind = \"bucket\"`. Can be changed in place.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{bucketPrefixValidator{}},
				PlanModifiers: []planmodifier.String{
					ccppm.LegacyStringDefault(""),
				},
			},
			"origin_status": schema.StringAttribute{
				MarkdownDescription: "State of the bucket origin, as last checked by the platform: null until the first " +
					"check (right after creation, and for `upstream` target groups), then `ok` when the bucket is " +
					"readable publicly, or `drift` when it is not (see `origin_error`).",
				Computed: true,
			},
			"origin_error": schema.StringAttribute{
				MarkdownDescription: "Explanation given when `origin_status` is `drift` (the bucket is marked public but " +
					"the storage does not make it publicly readable). Null otherwise.",
				Computed: true,
			},
			"algorithm": schema.StringAttribute{
				MarkdownDescription: "Load-balancing algorithm: `roundrobin` (default), `leastconn`, `source` " +
					"(client IP hash) or `random`.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("roundrobin"),
				Validators: []validator.String{stringvalidator.OneOf(appgwvalidators.Algorithms...)},
			},
			"hc_protocol": schema.StringAttribute{
				MarkdownDescription: "Health-check protocol: `http` (default), `https` or `tcp`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("http"),
				Validators:          []validator.String{stringvalidator.OneOf(appgwvalidators.HCProtocols...)},
			},
			"hc_method": schema.StringAttribute{
				MarkdownDescription: "HTTP method used for health checks. Ignored when `hc_protocol = tcp`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("GET"),
				Validators:          []validator.String{stringvalidator.OneOf(appgwvalidators.HCMethods...)},
			},
			"hc_path": schema.StringAttribute{
				MarkdownDescription: "Health-check URL path. Ignored when `hc_protocol = tcp`. Defaults to `/`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("/"),
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"hc_expect_status": schema.Int64Attribute{
				MarkdownDescription: "Expected HTTP status code from health-check responses (default `200`).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(200),
				Validators:          []validator.Int64{int64validator.Between(100, 599)},
			},
			"hc_interval_sec": schema.Int64Attribute{
				MarkdownDescription: "Health-check interval in seconds (default `5`).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(5),
				Validators:          []validator.Int64{int64validator.Between(1, 3600)},
			},
			"hc_timeout_sec": schema.Int64Attribute{
				MarkdownDescription: "Per-check timeout in seconds (default `3`).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(3),
				Validators:          []validator.Int64{int64validator.Between(1, 600)},
			},
			"hc_healthy_threshold": schema.Int64Attribute{
				MarkdownDescription: "Consecutive successful checks before a backend is marked healthy (default `2`).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(2),
				Validators:          []validator.Int64{int64validator.Between(1, 10)},
			},
			"hc_unhealthy_threshold": schema.Int64Attribute{
				MarkdownDescription: "Consecutive failed checks before a backend is marked unhealthy (default `3`).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(3),
				Validators:          []validator.Int64{int64validator.Between(1, 10)},
			},
			"sticky_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable cookie-based session stickiness.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"sticky_cookie_name": schema.StringAttribute{
				MarkdownDescription: "Cookie name used when `sticky_enabled = true`. Defaults to `CCPAPPGWSESSID`.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 50)},
			},
		},
	}
}

func (r *tgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func applyToModel(tg *client.AppGWTargetGroup, m *tgResourceModel) {
	m.ID = types.StringValue(tg.ID)
	m.AppGWID = types.StringValue(tg.AppGWID)
	m.Name = types.StringValue(tg.Name)
	m.Algorithm = types.StringValue(tg.Algorithm)
	m.HCProtocol = types.StringValue(tg.HCProtocol)
	m.HCMethod = types.StringValue(tg.HCMethod)
	m.HCPath = types.StringValue(tg.HCPath)
	m.HCExpectStatus = types.Int64Value(tg.HCExpectStatus)
	m.HCIntervalSec = types.Int64Value(tg.HCIntervalSec)
	m.HCTimeoutSec = types.Int64Value(tg.HCTimeoutSec)
	m.HCHealthyThreshold = types.Int64Value(tg.HCHealthyThreshold)
	m.HCUnhealthyThreshold = types.Int64Value(tg.HCUnhealthyThreshold)
	m.StickyEnabled = types.BoolValue(tg.StickyEnabled)
	if tg.StickyCookieName != nil {
		m.StickyCookieName = types.StringValue(*tg.StickyCookieName)
	} else {
		m.StickyCookieName = types.StringNull()
	}
	// An API that predates bucket targets sends no kind: it is an upstream pool.
	kind := tg.Kind
	if kind == "" {
		kind = kindUpstream
	}
	m.Kind = types.StringValue(kind)
	m.BucketID = types.StringPointerValue(tg.BucketID)
	m.BucketPrefix = types.StringValue(tg.BucketPrefix)
	m.OriginStatus = types.StringPointerValue(tg.OriginStatus)
	m.OriginError = types.StringPointerValue(tg.OriginError)
}

// isBucket tells whether the model describes a bucket target.
func (m *tgResourceModel) isBucket() bool { return m.Kind.ValueString() == kindBucket }

// serverFieldAttrs are the arguments that only make sense for a pool of servers.
var serverFieldAttrs = []string{
	"algorithm", "hc_protocol", "hc_method", "hc_path", "hc_expect_status",
	"hc_interval_sec", "hc_timeout_sec", "hc_healthy_threshold",
	"hc_unhealthy_threshold", "sticky_enabled", "sticky_cookie_name",
}

// ValidateConfig rejects, at plan time, combinations the platform would refuse.
func (r *tgResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var kind, bucketID, prefix types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("kind"), &kind)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("bucket_id"), &bucketID)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("bucket_prefix"), &prefix)...)
	if resp.Diagnostics.HasError() || kind.IsUnknown() {
		return
	}
	switch kind.ValueString() {
	case kindBucket:
		if bucketID.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("bucket_id"), "bucket_id is required",
				"A target group with kind = \"bucket\" must name the bucket to serve.")
		}
		for _, name := range serverFieldAttrs {
			var v attr.Value
			switch name {
			case "hc_expect_status", "hc_interval_sec", "hc_timeout_sec", "hc_healthy_threshold", "hc_unhealthy_threshold":
				var n types.Int64
				resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &n)...)
				v = n
			case "sticky_enabled":
				var b types.Bool
				resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &b)...)
				v = b
			default:
				var str types.String
				resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(name), &str)...)
				v = str
			}
			if !v.IsNull() {
				resp.Diagnostics.AddAttributeError(path.Root(name), name+" does not apply to a bucket target",
					"A bucket target has no servers, health check or session persistence: remove this argument "+
						"or use kind = \"upstream\".")
			}
		}
	default: // upstream (explicit or defaulted)
		if !bucketID.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("bucket_id"), "bucket_id requires kind = \"bucket\"",
				"A bucket can only be set on a target group of kind \"bucket\".")
		}
		if !prefix.IsNull() && !prefix.IsUnknown() && prefix.ValueString() != "" {
			resp.Diagnostics.AddAttributeError(path.Root("bucket_prefix"), "bucket_prefix requires kind = \"bucket\"",
				"A bucket folder can only be set on a target group of kind \"bucket\".")
		}
	}
}

func (r *tgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tgResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.AppGWTargetGroupCreateRequest{
		Name: plan.Name.ValueString(),
	}
	if plan.isBucket() {
		// A bucket target carries no server settings: send only what applies.
		kind := kindBucket
		createReq.Kind = &kind
		if !plan.BucketID.IsNull() && !plan.BucketID.IsUnknown() {
			v := plan.BucketID.ValueString()
			createReq.BucketID = &v
		}
		if !plan.BucketPrefix.IsNull() && !plan.BucketPrefix.IsUnknown() && plan.BucketPrefix.ValueString() != "" {
			v := plan.BucketPrefix.ValueString()
			createReq.BucketPrefix = &v
		}
		r.finishCreate(ctx, &plan, createReq, resp)
		return
	}
	if !plan.Algorithm.IsNull() && !plan.Algorithm.IsUnknown() {
		v := plan.Algorithm.ValueString()
		createReq.Algorithm = &v
	}
	if !plan.HCProtocol.IsNull() && !plan.HCProtocol.IsUnknown() {
		v := plan.HCProtocol.ValueString()
		createReq.HCProtocol = &v
	}
	if !plan.HCMethod.IsNull() && !plan.HCMethod.IsUnknown() {
		v := plan.HCMethod.ValueString()
		createReq.HCMethod = &v
	}
	if !plan.HCPath.IsNull() && !plan.HCPath.IsUnknown() {
		v := plan.HCPath.ValueString()
		createReq.HCPath = &v
	}
	if !plan.HCExpectStatus.IsNull() && !plan.HCExpectStatus.IsUnknown() {
		v := plan.HCExpectStatus.ValueInt64()
		createReq.HCExpectStatus = &v
	}
	if !plan.HCIntervalSec.IsNull() && !plan.HCIntervalSec.IsUnknown() {
		v := plan.HCIntervalSec.ValueInt64()
		createReq.HCIntervalSec = &v
	}
	if !plan.HCTimeoutSec.IsNull() && !plan.HCTimeoutSec.IsUnknown() {
		v := plan.HCTimeoutSec.ValueInt64()
		createReq.HCTimeoutSec = &v
	}
	if !plan.HCHealthyThreshold.IsNull() && !plan.HCHealthyThreshold.IsUnknown() {
		v := plan.HCHealthyThreshold.ValueInt64()
		createReq.HCHealthyThreshold = &v
	}
	if !plan.HCUnhealthyThreshold.IsNull() && !plan.HCUnhealthyThreshold.IsUnknown() {
		v := plan.HCUnhealthyThreshold.ValueInt64()
		createReq.HCUnhealthyThreshold = &v
	}
	if !plan.StickyEnabled.IsNull() && !plan.StickyEnabled.IsUnknown() {
		v := plan.StickyEnabled.ValueBool()
		createReq.StickyEnabled = &v
	}
	if !plan.StickyCookieName.IsNull() && !plan.StickyCookieName.IsUnknown() && plan.StickyCookieName.ValueString() != "" {
		v := plan.StickyCookieName.ValueString()
		createReq.StickyCookieName = &v
	}

	r.finishCreate(ctx, &plan, createReq, resp)
}

func (r *tgResource) finishCreate(ctx context.Context, plan *tgResourceModel, createReq client.AppGWTargetGroupCreateRequest, resp *resource.CreateResponse) {
	created, err := r.client.CreateAppGWTargetGroup(ctx, plan.AppGWID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AppGW target group", err.Error())
		return
	}
	applyToModel(created, plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *tgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tgResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetAppGWTargetGroup(ctx, state.AppGWID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read AppGW target group", err.Error())
		return
	}
	applyToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *tgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state tgResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var upd client.AppGWTargetGroupUpdateRequest
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
	}
	if state.isBucket() {
		// Only the name, the bucket and the folder can change on a bucket target.
		if !plan.BucketID.IsNull() && !plan.BucketID.IsUnknown() && !plan.BucketID.Equal(state.BucketID) {
			v := plan.BucketID.ValueString()
			upd.BucketID = &v
		}
		if !plan.BucketPrefix.IsNull() && !plan.BucketPrefix.IsUnknown() && !plan.BucketPrefix.Equal(state.BucketPrefix) {
			v := plan.BucketPrefix.ValueString()
			upd.BucketPrefix = &v
		}
		got, err := r.client.UpdateAppGWTargetGroup(ctx, state.AppGWID.ValueString(), state.ID.ValueString(), upd)
		if err != nil {
			resp.Diagnostics.AddError("Failed to update AppGW target group", err.Error())
			return
		}
		applyToModel(got, &plan)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	if !plan.Algorithm.Equal(state.Algorithm) {
		v := plan.Algorithm.ValueString()
		upd.Algorithm = &v
	}
	if !plan.HCProtocol.Equal(state.HCProtocol) {
		v := plan.HCProtocol.ValueString()
		upd.HCProtocol = &v
	}
	if !plan.HCMethod.Equal(state.HCMethod) {
		v := plan.HCMethod.ValueString()
		upd.HCMethod = &v
	}
	if !plan.HCPath.Equal(state.HCPath) {
		v := plan.HCPath.ValueString()
		upd.HCPath = &v
	}
	if !plan.HCExpectStatus.Equal(state.HCExpectStatus) {
		v := plan.HCExpectStatus.ValueInt64()
		upd.HCExpectStatus = &v
	}
	if !plan.HCIntervalSec.Equal(state.HCIntervalSec) {
		v := plan.HCIntervalSec.ValueInt64()
		upd.HCIntervalSec = &v
	}
	if !plan.HCTimeoutSec.Equal(state.HCTimeoutSec) {
		v := plan.HCTimeoutSec.ValueInt64()
		upd.HCTimeoutSec = &v
	}
	if !plan.HCHealthyThreshold.Equal(state.HCHealthyThreshold) {
		v := plan.HCHealthyThreshold.ValueInt64()
		upd.HCHealthyThreshold = &v
	}
	if !plan.HCUnhealthyThreshold.Equal(state.HCUnhealthyThreshold) {
		v := plan.HCUnhealthyThreshold.ValueInt64()
		upd.HCUnhealthyThreshold = &v
	}
	if !plan.StickyEnabled.Equal(state.StickyEnabled) {
		v := plan.StickyEnabled.ValueBool()
		upd.StickyEnabled = &v
	}
	if !plan.StickyCookieName.Equal(state.StickyCookieName) {
		v := ""
		if !plan.StickyCookieName.IsNull() && !plan.StickyCookieName.IsUnknown() {
			v = plan.StickyCookieName.ValueString()
		}
		upd.StickyCookieName = &v
	}
	var got *client.AppGWTargetGroup
	var err error
	if upd == (client.AppGWTargetGroupUpdateRequest{}) {
		// Nothing to send (for example a state written before `kind` existed
		// that now spells `kind = "upstream"`): just read the current state.
		got, err = r.client.GetAppGWTargetGroup(ctx, state.AppGWID.ValueString(), state.ID.ValueString())
	} else {
		got, err = r.client.UpdateAppGWTargetGroup(ctx, state.AppGWID.ValueString(), state.ID.ValueString(), upd)
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to update AppGW target group", err.Error())
		return
	}
	applyToModel(got, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *tgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state tgResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAppGWTargetGroup(ctx, state.AppGWID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete AppGW target group", err.Error())
	}
}

func (r *tgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<appgw_id>/<target_group_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("appgw_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
