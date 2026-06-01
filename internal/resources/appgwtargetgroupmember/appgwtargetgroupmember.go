// Package appgwtargetgroupmember implements the
// ccp_appgw_target_group_member resource — a single backend inside a
// target group. Backends are addressed either by container UUID, VM
// instance UUID or raw IP (exactly one of the three must be set —
// enforced via ValidateConfig).
//
// `target_group_id`, the target identifier and `port` are immutable —
// changes force a destroy + create. `weight` and `enabled` are PATCHable
// in place.
package appgwtargetgroupmember

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                    = (*memberResource)(nil)
	_ resource.ResourceWithConfigure       = (*memberResource)(nil)
	_ resource.ResourceWithImportState     = (*memberResource)(nil)
	_ resource.ResourceWithValidateConfig  = (*memberResource)(nil)
)

func New() resource.Resource { return &memberResource{} }

type memberResource struct{ client *client.Client }

type memberResourceModel struct {
	ID            types.String `tfsdk:"id"`
	AppGWID       types.String `tfsdk:"appgw_id"`
	TargetGroupID types.String `tfsdk:"target_group_id"`
	ContainerID   types.String `tfsdk:"container_id"`
	VMInstanceID  types.String `tfsdk:"vm_instance_id"`
	TargetIP      types.String `tfsdk:"target_ip"`
	Port          types.Int64  `tfsdk:"port"`
	Weight        types.Int64  `tfsdk:"weight"`
	Enabled       types.Bool   `tfsdk:"enabled"`
}

func (r *memberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_appgw_target_group_member"
}

func (r *memberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a single backend (member) inside a `ccp_appgw_target_group`. Exactly one of " +
			"`container_id`, `vm_instance_id` or `target_ip` must be set — checked at plan-time via " +
			"`ValidateConfig`.\n\n" +
			"~> **`appgw_id`, `target_group_id`, the target identifier and `port` are immutable.** Any change " +
			"forces a destroy + create.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the member.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"appgw_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_application_gateway`. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target_group_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent target group. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"container_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a `ccp_container_instance` used as backend. " +
					"Exactly one of `container_id`, `vm_instance_id` or `target_ip` must be set. **Immutable**.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vm_instance_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a `ccp_vm_instance` used as backend. **Immutable**.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target_ip": schema.StringAttribute{
				MarkdownDescription: "Raw IPv4/IPv6 address inside the gateway's VNet used as backend. **Immutable**.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Backend port (1-65535). **Immutable**.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifierRequiresReplace{}},
			},
			"weight": schema.Int64Attribute{
				MarkdownDescription: "Load-balancing weight (0-1000, default `100`). 0 drains the backend.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(100),
				Validators:          []validator.Int64{int64validator.Between(0, 1000)},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "When `false`, the backend is administratively disabled and skipped by the gateway " +
					"(useful for manual drain). Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
		},
	}
}

// int64planmodifierRequiresReplace forces replacement on any change to an
// int attribute (port). The framework stdlib does not export a
// `RequiresReplace()` for Int64 — we provide a tiny implementation.
type int64planmodifierRequiresReplace struct{}

func (int64planmodifierRequiresReplace) Description(_ context.Context) string {
	return "Any change to this attribute forces resource replacement."
}
func (m int64planmodifierRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}
func (int64planmodifierRequiresReplace) PlanModifyInt64(_ context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

func (r *memberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig enforces XOR on (container_id, vm_instance_id, target_ip).
// Per CLAUDE.md pitfall #4, we early-return when ANY of the three is
// Unknown — the planner may not have resolved a reference yet, and a
// false positive here would block every `terraform validate`.
func (r *memberResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg memberResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.ContainerID.IsUnknown() || cfg.VMInstanceID.IsUnknown() || cfg.TargetIP.IsUnknown() {
		return
	}
	setCount := 0
	if !cfg.ContainerID.IsNull() && cfg.ContainerID.ValueString() != "" {
		setCount++
	}
	if !cfg.VMInstanceID.IsNull() && cfg.VMInstanceID.ValueString() != "" {
		setCount++
	}
	if !cfg.TargetIP.IsNull() && cfg.TargetIP.ValueString() != "" {
		setCount++
	}
	switch setCount {
	case 0:
		resp.Diagnostics.AddError(
			"Missing backend target",
			"Exactly one of `container_id`, `vm_instance_id` or `target_ip` must be set.",
		)
	case 1:
		// OK
	default:
		resp.Diagnostics.AddError(
			"Ambiguous backend target",
			"Exactly one of `container_id`, `vm_instance_id` or `target_ip` may be set — got "+
				fmt.Sprintf("%d.", setCount),
		)
	}
}

func applyToModel(mm *client.AppGWTargetGroupMember, m *memberResourceModel) {
	m.ID = types.StringValue(mm.ID)
	m.TargetGroupID = types.StringValue(mm.TargetGroupID)
	m.Port = types.Int64Value(mm.Port)
	m.Weight = types.Int64Value(mm.Weight)
	m.Enabled = types.BoolValue(mm.Enabled)
	if mm.ContainerID != nil {
		m.ContainerID = types.StringValue(*mm.ContainerID)
	} else {
		m.ContainerID = types.StringNull()
	}
	if mm.VMInstanceID != nil {
		m.VMInstanceID = types.StringValue(*mm.VMInstanceID)
	} else {
		m.VMInstanceID = types.StringNull()
	}
	if mm.TargetIP != nil {
		m.TargetIP = types.StringValue(*mm.TargetIP)
	} else {
		m.TargetIP = types.StringNull()
	}
}

func (r *memberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan memberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.AppGWTargetGroupMemberCreateRequest{
		Port: plan.Port.ValueInt64(),
	}
	if !plan.ContainerID.IsNull() && !plan.ContainerID.IsUnknown() && plan.ContainerID.ValueString() != "" {
		v := plan.ContainerID.ValueString()
		createReq.ContainerID = &v
	}
	if !plan.VMInstanceID.IsNull() && !plan.VMInstanceID.IsUnknown() && plan.VMInstanceID.ValueString() != "" {
		v := plan.VMInstanceID.ValueString()
		createReq.VMInstanceID = &v
	}
	if !plan.TargetIP.IsNull() && !plan.TargetIP.IsUnknown() && plan.TargetIP.ValueString() != "" {
		v := plan.TargetIP.ValueString()
		createReq.TargetIP = &v
	}
	if !plan.Weight.IsNull() && !plan.Weight.IsUnknown() {
		v := plan.Weight.ValueInt64()
		createReq.Weight = &v
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		createReq.Enabled = &v
	}
	created, err := r.client.AddAppGWTargetGroupMember(ctx, plan.AppGWID.ValueString(), plan.TargetGroupID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to add target group member", err.Error())
		return
	}
	applyToModel(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state memberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetAppGWTargetGroupMember(ctx, state.AppGWID.ValueString(), state.TargetGroupID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read target group member", err.Error())
		return
	}
	applyToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *memberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state memberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var upd client.AppGWTargetGroupMemberUpdateRequest
	if !plan.Weight.Equal(state.Weight) {
		v := plan.Weight.ValueInt64()
		upd.Weight = &v
	}
	if !plan.Enabled.Equal(state.Enabled) {
		v := plan.Enabled.ValueBool()
		upd.Enabled = &v
	}
	got, err := r.client.UpdateAppGWTargetGroupMember(ctx, state.AppGWID.ValueString(), state.TargetGroupID.ValueString(), state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update target group member", err.Error())
		return
	}
	applyToModel(got, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state memberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.RemoveAppGWTargetGroupMember(ctx, state.AppGWID.ValueString(), state.TargetGroupID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to remove target group member", err.Error())
	}
}

func (r *memberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<appgw_id>/<target_group_id>/<member_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("appgw_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target_group_id"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[2])...)
}
