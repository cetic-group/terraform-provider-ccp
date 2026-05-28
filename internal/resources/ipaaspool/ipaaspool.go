// Package ipaaspool implements the ccp_ipaas_pool Terraform resource.
//
// **Admin only** — nécessite une API key ou JWT avec scope admin sur le tenant
// CETIC. Pour les utilisateurs CCP standard, l'IPaaS pool est invisible.
//
// Pool BYOIP routé via edge Scaleway WG+BGP. Voir CLAUDE.md racine "IPaaS".
package ipaaspool

import (
	"context"
	"fmt"

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
	_ resource.Resource                = (*poolResource)(nil)
	_ resource.ResourceWithConfigure   = (*poolResource)(nil)
	_ resource.ResourceWithImportState = (*poolResource)(nil)
)

func New() resource.Resource { return &poolResource{} }

type poolResource struct{ client *client.Client }

type poolResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Region    types.String `tfsdk:"region"`
	CIDR      types.String `tfsdk:"cidr"`
	Gateway   types.String `tfsdk:"gateway"`
	Kind      types.String `tfsdk:"kind"`
	EdgeID    types.String `tfsdk:"edge_id"`
	IsActive  types.Bool   `tfsdk:"is_active"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *poolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_ipaas_pool"
}

func (r *poolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "**Admin only** — IPaaS BYOIP pool routé via edge Scaleway. " +
			"L'API key utilisée doit avoir le scope `admin`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"region": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cidr": schema.StringAttribute{
				MarkdownDescription: "BYOIP block (ex: 163.172.232.192/27).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"gateway": schema.StringAttribute{
				MarkdownDescription: "Gateway du pool (1re IP utilisable, réservée par le routage Scaleway).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"kind": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"edge_id": schema.StringAttribute{
				MarkdownDescription: "UUID de l'edge IPaaS (Dedibox Scaleway). Optionnel — peut être assigné après création.",
				Optional:            true,
				Computed:            true,
			},
			"is_active": schema.BoolAttribute{
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

func (r *poolResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func setState(m *poolResourceModel, p *client.IpaasPool) {
	m.ID = types.StringValue(p.ID)
	m.Region = types.StringValue(p.Region)
	m.CIDR = types.StringValue(p.CIDR)
	m.Gateway = types.StringValue(p.Gateway)
	m.Kind = types.StringValue(p.Kind)
	m.IsActive = types.BoolValue(p.IsActive)
	m.CreatedAt = types.StringValue(p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	if p.EdgeID != nil {
		m.EdgeID = types.StringValue(*p.EdgeID)
	} else {
		m.EdgeID = types.StringNull()
	}
}

func (r *poolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.IpaasPoolCreateRequest{
		Region:   plan.Region.ValueString(),
		CIDR:     plan.CIDR.ValueString(),
		Gateway:  plan.Gateway.ValueString(),
		IsActive: plan.IsActive.ValueBool(),
	}
	if !plan.EdgeID.IsNull() && !plan.EdgeID.IsUnknown() {
		v := plan.EdgeID.ValueString()
		createReq.EdgeID = &v
	}
	created, err := r.client.CreateIpaasPool(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create IPaaS pool", err.Error())
		return
	}
	setState(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *poolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetIpaasPool(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read IPaaS pool", err.Error())
		return
	}
	setState(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *poolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var upd client.IpaasPoolUpdateRequest
	if !plan.IsActive.Equal(state.IsActive) {
		v := plan.IsActive.ValueBool()
		upd.IsActive = &v
	}
	if !plan.EdgeID.Equal(state.EdgeID) {
		if plan.EdgeID.IsNull() {
			empty := ""
			upd.EdgeID = &empty
		} else {
			v := plan.EdgeID.ValueString()
			upd.EdgeID = &v
		}
	}
	updated, err := r.client.UpdateIpaasPool(ctx, state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update IPaaS pool", err.Error())
		return
	}
	setState(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *poolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteIpaasPool(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete IPaaS pool", err.Error())
	}
}

func (r *poolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
