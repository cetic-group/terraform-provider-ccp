// Package organization implements the ccp_organization Terraform resource.
//
// Une org est un espace logique qui scope les ressources, les membres et la
// facturation. L'org `default` du tenant ne peut pas être supprimée.
package organization

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
	_ resource.Resource                = (*orgResource)(nil)
	_ resource.ResourceWithConfigure   = (*orgResource)(nil)
	_ resource.ResourceWithImportState = (*orgResource)(nil)
)

func New() resource.Resource { return &orgResource{} }

type orgResource struct{ client *client.Client }

type orgResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	IsDefault        types.Bool   `tfsdk:"is_default"`
	HasPaymentMethod types.Bool   `tfsdk:"has_payment_method"`
	HasSubscription  types.Bool   `tfsdk:"has_subscription"`
	Tags             types.List   `tfsdk:"tags"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *orgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_organization"
}

func (r *orgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "CETIC Cloud organization (logical workspace for resources, members, billing). " +
			"The default org of a tenant is created automatically and cannot be managed by Terraform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"is_default": schema.BoolAttribute{
				Computed: true,
			},
			"has_payment_method": schema.BoolAttribute{Computed: true},
			"has_subscription":   schema.BoolAttribute{Computed: true},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *orgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func stateFromAPI(ctx context.Context, o *client.OrganizationResource) (orgResourceModel, []string) {
	m := orgResourceModel{
		ID:               types.StringValue(o.ID),
		Name:             types.StringValue(o.Name),
		IsDefault:        types.BoolValue(o.IsDefault),
		HasPaymentMethod: types.BoolValue(o.HasPaymentMethod),
		HasSubscription:  types.BoolValue(o.HasSubscription),
		CreatedAt:        types.StringValue(o.CreatedAt.Format("2006-01-02T15:04:05Z07:00")),
	}
	if o.Description != nil {
		m.Description = types.StringValue(*o.Description)
	} else {
		m.Description = types.StringNull()
	}
	tags, diag := types.ListValueFrom(ctx, types.StringType, o.Tags)
	var diagStrs []string
	if diag.HasError() {
		for _, d := range diag.Errors() {
			diagStrs = append(diagStrs, d.Summary())
		}
	}
	m.Tags = tags
	return m, diagStrs
}

func (r *orgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.OrganizationCreateRequest{Name: plan.Name.ValueString()}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		createReq.Description = &v
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags := []string{}
		plan.Tags.ElementsAs(ctx, &tags, false)
		createReq.Tags = tags
	}
	created, err := r.client.CreateOrganization(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create organization", err.Error())
		return
	}
	state, _ := stateFromAPI(ctx, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetOrganization(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read organization", err.Error())
		return
	}
	newState, _ := stateFromAPI(ctx, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *orgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state orgResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var upd client.OrganizationUpdateRequest
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
	}
	if !plan.Description.Equal(state.Description) {
		v := plan.Description.ValueString()
		upd.Description = &v
	}
	if !plan.Tags.Equal(state.Tags) {
		tags := []string{}
		if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
			plan.Tags.ElementsAs(ctx, &tags, false)
		}
		upd.Tags = tags
	}
	updated, err := r.client.UpdateOrganization(ctx, state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update organization", err.Error())
		return
	}
	newState, _ := stateFromAPI(ctx, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *orgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteOrganization(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete organization", err.Error())
	}
}

func (r *orgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
