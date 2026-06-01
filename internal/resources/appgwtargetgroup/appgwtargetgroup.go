// Package appgwtargetgroup implements the ccp_appgw_target_group
// Terraform resource — a pool of backends with load-balancing algorithm
// and health-check configuration, used by routes via `target_group_id`.
//
// Members (the actual backends) are managed via ccp_appgw_target_group_member.
package appgwtargetgroup

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/appgwvalidators"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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
	_ resource.Resource                = (*tgResource)(nil)
	_ resource.ResourceWithConfigure   = (*tgResource)(nil)
	_ resource.ResourceWithImportState = (*tgResource)(nil)
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
}

func (r *tgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_appgw_target_group"
}

func (r *tgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a target group on a `ccp_application_gateway` — a pool of backends with " +
			"load-balancing algorithm and L7 health-check configuration. Routes reference target groups via " +
			"`target_group_id`. Members are managed via `ccp_appgw_target_group_member`.",
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
			"algorithm": schema.StringAttribute{
				MarkdownDescription: "Load-balancing algorithm: `roundrobin` (default), `leastconn` or `source` " +
					"(client IP hash).",
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

	created, err := r.client.CreateAppGWTargetGroup(ctx, plan.AppGWID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AppGW target group", err.Error())
		return
	}
	applyToModel(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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
	got, err := r.client.UpdateAppGWTargetGroup(ctx, state.AppGWID.ValueString(), state.ID.ValueString(), upd)
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
