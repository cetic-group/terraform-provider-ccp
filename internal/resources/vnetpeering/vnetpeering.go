// Package vnetpeering implements the ccp_vnet_peering Terraform resource.
//
// Permet le routage L3 entre deux VNets — qu'ils soient dans le même VPC ou
// dans deux VPCs différents (intra-tenant). C'est le SEUL type de peering
// exposé par CETIC Cloud : il n'y a pas de "vpc_peering" qui fédèrerait
// tous les VNets de 2 VPCs ; il faut peer chaque couple de VNets explicitement.
//
// Le backend impose un ordre canonique `vnet_a_id < vnet_b_id` (CheckConstraint
// + UniqueConstraint en DB). Le client TF normalise automatiquement l'ordre
// pour que l'utilisateur puisse passer les UUIDs dans n'importe quel sens.
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
	_ resource.ResourceWithModifyPlan  = (*peerResource)(nil)
)

func New() resource.Resource { return &peerResource{} }

type peerResource struct{ client *client.Client }

type peerResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	VnetAID   types.String `tfsdk:"vnet_a_id"`
	VnetBID   types.String `tfsdk:"vnet_b_id"`
	Tags      types.List   `tfsdk:"tags"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`
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
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name for the peering (2-100 chars).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_a_id": schema.StringAttribute{
				MarkdownDescription: "UUID of one VNet (order doesn't matter — provider normalizes a < b at plan time).",
				Required:            true,
			},
			"vnet_b_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the other VNet (different from vnet_a_id, can be in another VPC).",
				Required:            true,
			},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
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

// canonicalOrder returns (a, b) such that a < b lexicographically — required
// by the backend's CheckConstraint(vnet_a_id < vnet_b_id).
func canonicalOrder(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func (r *peerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan peerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	a, b := canonicalOrder(plan.VnetAID.ValueString(), plan.VnetBID.ValueString())
	if a == b {
		resp.Diagnostics.AddError("Invalid peering", "vnet_a_id and vnet_b_id must be different")
		return
	}

	tags := []string{}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		plan.Tags.ElementsAs(ctx, &tags, false)
	}

	created, err := r.client.CreateVnetPeering(ctx, client.VnetPeeringCreateRequest{
		Name:    plan.Name.ValueString(),
		VnetAID: a,
		VnetBID: b,
		Tags:    tags,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VNet peering", err.Error())
		return
	}

	plan.ID = types.StringValue(created.ID)
	plan.VnetAID = types.StringValue(created.VnetAID)
	plan.VnetBID = types.StringValue(created.VnetBID)
	plan.Status = types.StringValue(created.Status)
	plan.CreatedAt = types.StringValue(created.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))

	tagsList, _ := types.ListValueFrom(ctx, types.StringType, created.Tags)
	plan.Tags = tagsList

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
	state.Name = types.StringValue(got.Name)
	state.VnetAID = types.StringValue(got.VnetAID)
	state.VnetBID = types.StringValue(got.VnetBID)
	state.Status = types.StringValue(got.Status)
	state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))

	tagsList, _ := types.ListValueFrom(ctx, types.StringType, got.Tags)
	state.Tags = tagsList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *peerResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported",
		"VNet peering attributes force replacement.")
}

// ModifyPlan normalizes (vnet_a_id, vnet_b_id) to canonical order (a < b) at
// plan time so:
//   - the plan matches what Create returns from the backend (no
//     "Provider produced inconsistent result after apply" error);
//   - swapping a/b in HCL while keeping the same pair is a no-op (no replace);
//   - changing the actual pair triggers a forced replace via resp.RequiresReplace.
func (r *peerResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Destroy: no plan to modify.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan peerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Both UUIDs must be known to canonicalize.
	if plan.VnetAID.IsUnknown() || plan.VnetBID.IsUnknown() ||
		plan.VnetAID.IsNull() || plan.VnetBID.IsNull() {
		return
	}

	a, b := canonicalOrder(plan.VnetAID.ValueString(), plan.VnetBID.ValueString())
	if a != plan.VnetAID.ValueString() || b != plan.VnetBID.ValueString() {
		plan.VnetAID = types.StringValue(a)
		plan.VnetBID = types.StringValue(b)
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Force replace if the canonical pair differs from prior state. State is
	// already canonical (Create/Read both write canonical), so we compare directly.
	if !req.State.Raw.IsNull() {
		var state peerResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if state.VnetAID.ValueString() != a || state.VnetBID.ValueString() != b {
			resp.RequiresReplace = append(resp.RequiresReplace,
				path.Root("vnet_a_id"),
				path.Root("vnet_b_id"),
			)
		}
	}
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
