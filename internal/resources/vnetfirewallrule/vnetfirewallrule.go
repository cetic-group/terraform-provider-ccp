// Package vnetfirewallrule implements the ccp_vnet_firewall_rule resource.
//
// Règle de firewall pour un VNet CETIC Cloud (isolation per-VM Proxmox).
// Modifiable : enabled, position. Les autres champs (direction, action,
// proto, CIDRs, dport) requièrent une recréation.
package vnetfirewallrule

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*fwResource)(nil)
	_ resource.ResourceWithConfigure   = (*fwResource)(nil)
	_ resource.ResourceWithImportState = (*fwResource)(nil)
)

func New() resource.Resource { return &fwResource{} }

type fwResource struct{ client *client.Client }

type fwModel struct {
	ID         types.String `tfsdk:"id"`
	VnetID     types.String `tfsdk:"vnet_id"`
	Direction  types.String `tfsdk:"direction"`
	Action     types.String `tfsdk:"action"`
	Proto      types.String `tfsdk:"proto"`
	SourceCIDR types.String `tfsdk:"source_cidr"`
	DestCIDR   types.String `tfsdk:"dest_cidr"`
	Dport      types.String `tfsdk:"dport"`
	Comment    types.String `tfsdk:"comment"`
	Position   types.Int64  `tfsdk:"position"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (r *fwResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vnet_firewall_rule"
}

func (r *fwResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Règle de firewall d'un VNet CETIC Cloud. " +
			"Nécessite que l'isolation soit activée sur le VNet.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"direction": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`in` ou `out`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"action": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`ACCEPT` ou `DROP`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"proto": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Protocole : `tcp`, `udp`, `icmp`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"source_cidr": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dest_cidr": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dport": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Port de destination, ex : `80`, `443`, `8000-9000`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"comment": schema.StringAttribute{
				Optional: true,
			},
			"position": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Priorité de la règle (ordre d'évaluation croissant).",
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *fwResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = c
}

func stateFrom(v *client.VnetFirewallRule) fwModel {
	m := fwModel{
		ID:        types.StringValue(v.ID),
		VnetID:    types.StringValue(v.VnetID),
		Direction: types.StringValue(v.Direction),
		Action:    types.StringValue(v.Action),
		Position:  types.Int64Value(int64(v.Position)),
		Enabled:   types.BoolValue(v.Enabled),
		CreatedAt: types.StringValue(v.CreatedAt),
	}
	if v.Proto != nil {
		m.Proto = types.StringValue(*v.Proto)
	} else {
		m.Proto = types.StringNull()
	}
	if v.SourceCIDR != nil {
		m.SourceCIDR = types.StringValue(*v.SourceCIDR)
	} else {
		m.SourceCIDR = types.StringNull()
	}
	if v.DestCIDR != nil {
		m.DestCIDR = types.StringValue(*v.DestCIDR)
	} else {
		m.DestCIDR = types.StringNull()
	}
	if v.Dport != nil {
		m.Dport = types.StringValue(*v.Dport)
	} else {
		m.Dport = types.StringNull()
	}
	if v.Comment != nil {
		m.Comment = types.StringValue(*v.Comment)
	} else {
		m.Comment = types.StringNull()
	}
	return m
}

func (r *fwResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fwModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cr := client.VnetFirewallRuleCreateRequest{
		Direction: plan.Direction.ValueString(),
		Action:    plan.Action.ValueString(),
		Enabled:   plan.Enabled.ValueBool(),
	}
	if !plan.Proto.IsNull() && !plan.Proto.IsUnknown() {
		v := plan.Proto.ValueString()
		cr.Proto = &v
	}
	if !plan.SourceCIDR.IsNull() && !plan.SourceCIDR.IsUnknown() {
		v := plan.SourceCIDR.ValueString()
		cr.SourceCIDR = &v
	}
	if !plan.DestCIDR.IsNull() && !plan.DestCIDR.IsUnknown() {
		v := plan.DestCIDR.ValueString()
		cr.DestCIDR = &v
	}
	if !plan.Dport.IsNull() && !plan.Dport.IsUnknown() {
		v := plan.Dport.ValueString()
		cr.Dport = &v
	}
	if !plan.Comment.IsNull() && !plan.Comment.IsUnknown() {
		v := plan.Comment.ValueString()
		cr.Comment = &v
	}
	if !plan.Position.IsNull() && !plan.Position.IsUnknown() {
		cr.Position = int(plan.Position.ValueInt64())
	}
	rule, err := r.client.CreateVnetFirewallRule(ctx, plan.VnetID.ValueString(), cr)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VNet firewall rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFrom(rule))...)
}

func (r *fwResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state fwModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rule, err := r.client.GetVnetFirewallRule(ctx, state.VnetID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VNet firewall rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFrom(rule))...)
}

func (r *fwResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state fwModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only enabled, position, and comment are mutable.
	patch := map[string]any{
		"enabled":  plan.Enabled.ValueBool(),
		"position": int(plan.Position.ValueInt64()),
	}
	if !plan.Comment.IsNull() && !plan.Comment.IsUnknown() {
		patch["comment"] = plan.Comment.ValueString()
	}
	rule, err := r.client.UpdateVnetFirewallRule(ctx, state.VnetID.ValueString(), state.ID.ValueString(), patch)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update VNet firewall rule", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFrom(rule))...)
}

func (r *fwResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state fwModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVnetFirewallRule(ctx, state.VnetID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VNet firewall rule", err.Error())
		return
	}
}

func (r *fwResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: vnet_id/rule_id
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "Format: vnet_id/rule_id")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vnet_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
