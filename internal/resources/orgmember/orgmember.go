// Package orgmember implements the ccp_org_member Terraform resource.
//
// Invitation par email d'un membre dans le tenant courant. Si le compte
// existe déjà côté CL, le linkage est automatique ; sinon le membre devra
// créer son compte avec cet email pour que le linkage s'active.
package orgmember

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*memberResource)(nil)
	_ resource.ResourceWithConfigure   = (*memberResource)(nil)
	_ resource.ResourceWithImportState = (*memberResource)(nil)
)

func New() resource.Resource { return &memberResource{} }

type memberResource struct{ client *client.Client }

type memberResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Email      types.String `tfsdk:"email"`
	Role       types.String `tfsdk:"role"`
	Accepted   types.Bool   `tfsdk:"accepted"`
	AcceptedAt types.String `tfsdk:"accepted_at"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (r *memberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_member"
}

func (r *memberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Invite a user (by email) as member of the current tenant. Roles: " +
			"`admin` (everything except billing/delete-org), `member` (CRUD resources), `viewer` (read-only).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("admin", "member", "viewer"),
				},
			},
			"accepted": schema.BoolAttribute{
				MarkdownDescription: "True once the invitee has logged in with the matching email.",
				Computed:            true,
			},
			"accepted_at": schema.StringAttribute{
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

func setState(m *memberResourceModel, x *client.OrgMember) {
	m.ID = types.StringValue(x.ID)
	m.Email = types.StringValue(x.Email)
	m.Role = types.StringValue(x.Role)
	m.Accepted = types.BoolValue(x.AcceptedAt != nil)
	if x.AcceptedAt != nil {
		m.AcceptedAt = types.StringValue(*x.AcceptedAt)
	} else {
		m.AcceptedAt = types.StringNull()
	}
	m.CreatedAt = types.StringValue(x.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
}

func (r *memberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan memberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateOrgMember(ctx, client.OrgMemberCreateRequest{
		Email: plan.Email.ValueString(),
		Role:  plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create org member", err.Error())
		return
	}
	setState(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state memberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetOrgMember(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read org member", err.Error())
		return
	}
	setState(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *memberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state memberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Role.Equal(state.Role) {
		updated, err := r.client.UpdateOrgMember(ctx, state.ID.ValueString(), client.OrgMemberUpdateRequest{
			Role: plan.Role.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to update role", err.Error())
			return
		}
		setState(&plan, updated)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *memberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state memberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteOrgMember(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete org member", err.Error())
	}
}

func (r *memberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
