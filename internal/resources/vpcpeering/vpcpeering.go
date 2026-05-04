// Package vpcpeering implements the ccp_vpc_peering Terraform resource.
//
// Peering inter-VPC du même tenant (ou cross-tenant via flow d'invitation —
// non couvert TF). Crée un canal de routage entre 2 VPCs.
package vpcpeering

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
	_ resource.Resource                = (*peerResource)(nil)
	_ resource.ResourceWithConfigure   = (*peerResource)(nil)
	_ resource.ResourceWithImportState = (*peerResource)(nil)
)

func New() resource.Resource { return &peerResource{} }

type peerResource struct{ client *client.Client }

type peerResourceModel struct {
	ID             types.String `tfsdk:"id"`
	RequesterVpcID types.String `tfsdk:"requester_vpc_id"`
	AccepterVpcID  types.String `tfsdk:"accepter_vpc_id"`
	Status         types.String `tfsdk:"status"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (r *peerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vpc_peering"
}

func (r *peerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "VPC peering (intra-tenant). Auto-accepté côté CL pour les peerings " +
			"entre VPCs du même tenant. Cross-tenant nécessite un flow d'invitation manuel non géré par TF.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"requester_vpc_id": schema.StringAttribute{
				MarkdownDescription: "VPC à partir duquel la demande est émise (le tenant doit en être propriétaire).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"accepter_vpc_id": schema.StringAttribute{
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
	created, err := r.client.CreateVpcPeering(ctx, plan.RequesterVpcID.ValueString(), client.VpcPeeringCreateRequest{
		AccepterVpcID: plan.AccepterVpcID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VPC peering", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ID)
	plan.RequesterVpcID = types.StringValue(created.RequesterVpcID)
	plan.AccepterVpcID = types.StringValue(created.AccepterVpcID)
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
	got, err := r.client.GetVpcPeering(ctx, state.RequesterVpcID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VPC peering", err.Error())
		return
	}
	state.Status = types.StringValue(got.Status)
	state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *peerResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "All attributes force replacement.")
}

func (r *peerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state peerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVpcPeering(ctx, state.RequesterVpcID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VPC peering", err.Error())
	}
}

// ImportState : "<requester_vpc_id>/<peering_id>"
func (r *peerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Import ID format", "Expected `<requester_vpc_id>/<peering_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("requester_vpc_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
