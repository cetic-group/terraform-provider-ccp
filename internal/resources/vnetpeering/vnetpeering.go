// Package vnetpeering implements the ccp_vnet_peering Terraform resource.
//
// Permet le routage L3 entre deux VNets (intra-VPC ou inter-VPC du même
// tenant). Pour le peering inter-tenant, voir le flow d'invitation côté API
// (non couvert par TF — opération multi-comptes).
package vnetpeering

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*peerResource)(nil)
	_ resource.ResourceWithConfigure   = (*peerResource)(nil)
	_ resource.ResourceWithImportState = (*peerResource)(nil)
)

func New() resource.Resource { return &peerResource{} }

type peerResource struct{ client *client.Client }

type peerResourceModel struct {
	ID           types.String `tfsdk:"id"`
	SourceVnetID types.String `tfsdk:"source_vnet_id"`
	TargetVnetID types.String `tfsdk:"target_vnet_id"`
	Status       types.String `tfsdk:"status"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (r *peerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vnet_peering"
}

func (r *peerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "L3 peering between two VNets — same tenant, intra-VPC or inter-VPC. " +
			"Both VNets must already exist. The peering is symmetric (only declare once).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"source_vnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target_vnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *peerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *peerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan peerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateVnetPeering(ctx, client.VnetPeeringCreateRequest{
		SourceVnetID: plan.SourceVnetID.ValueString(),
		TargetVnetID: plan.TargetVnetID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VNet peering", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ID)
	plan.Status = types.StringValue(created.Status)
	plan.CreatedAt = types.StringValue(created.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *peerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state peerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetVnetPeering(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VNet peering", err.Error())
		return
	}
	state.SourceVnetID = types.StringValue(got.SourceVnetID)
	state.TargetVnetID = types.StringValue(got.TargetVnetID)
	state.Status = types.StringValue(got.Status)
	state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *peerResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported",
		"VNet peering attributes force replacement.")
}

func (r *peerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state peerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVnetPeering(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VNet peering", err.Error())
	}
}

func (r *peerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
