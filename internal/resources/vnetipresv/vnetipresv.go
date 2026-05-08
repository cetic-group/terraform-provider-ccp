// Package vnetipresv implements the ccp_vnet_ip_reservation resource.
//
// Réservation d'une plage IP privée dans un VNet CETIC Cloud.
// La ressource est immutable — toute modification force la recréation.
package vnetipresv

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*resvResource)(nil)
	_ resource.ResourceWithConfigure   = (*resvResource)(nil)
	_ resource.ResourceWithImportState = (*resvResource)(nil)
)

func New() resource.Resource { return &resvResource{} }

type resvResource struct{ client *client.Client }

type resvModel struct {
	ID          types.String `tfsdk:"id"`
	VnetID      types.String `tfsdk:"vnet_id"`
	Name        types.String `tfsdk:"name"`
	IP          types.String `tfsdk:"ip"`
	RangeEnd    types.String `tfsdk:"range_end"`
	Description types.String `tfsdk:"description"`
	IPCount     types.Int64  `tfsdk:"ip_count"`
	Kind        types.String `tfsdk:"kind"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *resvResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vnet_ip_reservation"
}

func (r *resvResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Réservation d'une IP ou plage d'IPs privées dans un VNet. " +
			"Immutable — toute modification force la recréation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ip": schema.StringAttribute{
				Required:      true,
				MarkdownDescription: "Adresse IP de début (ou IP unique si `range_end` absent).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"range_end": schema.StringAttribute{
				Optional:      true,
				MarkdownDescription: "Adresse IP de fin pour une réservation de plage.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ip_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Nombre d'IPs couvertes par la réservation. Renommé depuis `count` dans v0.5.4 (collision avec le meta-argument Terraform).",
			},
			"kind": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Type de réservation : `single` ou `range`.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *resvResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func stateFrom(v *client.VnetIpReservation) resvModel {
	m := resvModel{
		ID:        types.StringValue(v.ID),
		VnetID:    types.StringValue(v.VnetID),
		Name:      types.StringValue(v.Name),
		IP:        types.StringValue(v.IP),
		IPCount:   types.Int64Value(int64(v.Count)),
		Kind:      types.StringValue(v.Kind),
		CreatedAt: types.StringValue(v.CreatedAt),
	}
	if v.RangeEnd != nil {
		m.RangeEnd = types.StringValue(*v.RangeEnd)
	} else {
		m.RangeEnd = types.StringNull()
	}
	if v.Description != nil {
		m.Description = types.StringValue(*v.Description)
	} else {
		m.Description = types.StringNull()
	}
	return m
}

func (r *resvResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resvModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cr := client.VnetIpReservationCreateRequest{
		Name: plan.Name.ValueString(),
		IP:   plan.IP.ValueString(),
	}
	if !plan.RangeEnd.IsNull() && !plan.RangeEnd.IsUnknown() {
		v := plan.RangeEnd.ValueString()
		cr.RangeEnd = &v
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		cr.Description = &v
	}
	resv, err := r.client.CreateVnetIpReservation(ctx, plan.VnetID.ValueString(), cr)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VNet IP reservation", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFrom(resv))...)
}

func (r *resvResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state resvModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resv, err := r.client.GetVnetIpReservation(ctx, state.VnetID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VNet IP reservation", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFrom(resv))...)
}

func (r *resvResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Immutable resource", "VNet IP reservations are immutable. Use replace.")
}

func (r *resvResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state resvModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVnetIpReservation(ctx, state.VnetID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VNet IP reservation", err.Error())
		return
	}
}

func (r *resvResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: vnet_id/reservation_id
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "Format: vnet_id/reservation_id")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vnet_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
