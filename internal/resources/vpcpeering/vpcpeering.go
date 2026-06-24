// Package vpcpeering implements the ccp_vpc_peering Terraform resource.
//
// Peering inter-VPC : connecte deux VPCs entiers au niveau L3. Tout le trafic
// de VPC-A peut atteindre tout le trafic de VPC-B sur IPs privées, et vice
// versa. Le backend impose un ordre canonique vpc_a_id < vpc_b_id (CheckConstraint
// + UniqueConstraint en DB). Le provider normalise automatiquement l'ordre.
package vpcpeering

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
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
	Name         types.String `tfsdk:"name"`
	VpcAID       types.String `tfsdk:"vpc_a_id"`
	VpcBID       types.String `tfsdk:"vpc_b_id"`
	Tags         types.List   `tfsdk:"tags"`
	Status       types.String `tfsdk:"status"`
	TenantID     types.String `tfsdk:"tenant_id"`
	ErrorMessage types.String `tfsdk:"error_message"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (r *peerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vpc_peering"
}

func (r *peerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Peering inter-VPC : connecte deux VPCs entiers (tout le VPC voit le pair) " +
			"au niveau L3, en IPs privées, sans traverser l'internet public. " +
			"Le peering est symétrique — ne déclarez qu'une seule ressource par couple de VPCs.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Nom lisible du peering (2-100 caractères). Tout changement force un remplacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vpc_a_id": schema.StringAttribute{
				MarkdownDescription: "UUID du premier VPC. L'ordre à la création est libre (le provider normalise a < b avant envoi à l'API), " +
					"mais une fois stocké, intervertir `vpc_a_id` et `vpc_b_id` en HCL force un remplacement.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vpc_b_id": schema.StringAttribute{
				MarkdownDescription: "UUID du second VPC (différent de vpc_a_id). Tout changement force un remplacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
			"tenant_id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"error_message": schema.StringAttribute{
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

// canonicalOrder returns (a, b) such that a <= b lexicographically — required
// by the backend's CheckConstraint(vpc_a_id < vpc_b_id).
func canonicalOrder(a, b string) (string, string) {
	if a <= b {
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

	a, b := canonicalOrder(plan.VpcAID.ValueString(), plan.VpcBID.ValueString())
	if a == b {
		resp.Diagnostics.AddError("Invalid peering", "vpc_a_id and vpc_b_id must be different")
		return
	}

	tags := []string{}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		plan.Tags.ElementsAs(ctx, &tags, false)
	}

	created, err := r.client.CreateVpcPeering(ctx, client.VpcPeeringCreateRequest{
		Name:   plan.Name.ValueString(),
		VpcAID: a,
		VpcBID: b,
		Tags:   tags,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VPC peering", err.Error())
		return
	}

	plan.ID = types.StringValue(created.ID)
	// Intentionally NOT overwriting plan.VpcAID / plan.VpcBID with the
	// canonical values returned by the API: Terraform requires Required
	// attributes to keep the value the user wrote in HCL, otherwise it
	// fails with "Provider produced invalid plan" / "Provider produced
	// inconsistent result after apply". State preserves the user's order;
	// the API only sees the canonical pair.
	plan.Status = types.StringValue(created.Status)
	plan.CreatedAt = types.StringValue(created.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))

	if created.TenantID != "" {
		plan.TenantID = types.StringValue(created.TenantID)
	}
	if created.ErrorMessage != nil {
		plan.ErrorMessage = types.StringValue(*created.ErrorMessage)
	} else {
		plan.ErrorMessage = types.StringValue("")
	}

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
	got, err := r.client.GetVpcPeering(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VPC peering", err.Error())
		return
	}
	state.Name = types.StringValue(got.Name)
	// Preserve the user's chosen order if the API reports the same pair.
	// The API always returns canonical (a <= b); state may have either order
	// (whatever the user originally wrote). Overwriting unconditionally
	// would create perpetual drift for users who happen to write b > a.
	sa, sb := canonicalOrder(state.VpcAID.ValueString(), state.VpcBID.ValueString())
	if sa != got.VpcAID || sb != got.VpcBID {
		// Genuine drift — the pair changed out-of-band. Adopt the API's view.
		state.VpcAID = types.StringValue(got.VpcAID)
		state.VpcBID = types.StringValue(got.VpcBID)
	}
	state.Status = types.StringValue(got.Status)
	state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))

	if got.TenantID != "" {
		state.TenantID = types.StringValue(got.TenantID)
	}
	if got.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*got.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringValue("")
	}

	tagsList, _ := types.ListValueFrom(ctx, types.StringType, got.Tags)
	state.Tags = tagsList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *peerResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported",
		"VPC peering attributes force replacement.")
}

func (r *peerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state peerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVpcPeering(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VPC peering", err.Error())
	}
}

func (r *peerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
