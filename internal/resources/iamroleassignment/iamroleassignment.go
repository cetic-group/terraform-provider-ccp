// Package iamroleassignment implements the ccp_iam_role_assignment Terraform resource.
//
// An assignment attaches a role to a principal of one of the 4 types:
// `org_member`, `api_key`, `service_account`, `ccks_workload`. All
// attributes (role_id, principal_*, expires_at) force replacement — there
// is no PATCH endpoint and v1 keeps the model immutable.
package iamroleassignment

import (
	"context"
	"fmt"
	"time"

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
	_ resource.Resource                = (*iamRoleAssignmentResource)(nil)
	_ resource.ResourceWithConfigure   = (*iamRoleAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*iamRoleAssignmentResource)(nil)
)

// New returns the resource factory used by `provider.Resources()`.
func New() resource.Resource { return &iamRoleAssignmentResource{} }

type iamRoleAssignmentResource struct{ client *client.Client }

type iamRoleAssignmentResourceModel struct {
	ID            types.String `tfsdk:"id"`
	RoleID        types.String `tfsdk:"role_id"`
	PrincipalType types.String `tfsdk:"principal_type"`
	PrincipalID   types.String `tfsdk:"principal_id"`
	ExpiresAt     types.String `tfsdk:"expires_at"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func (r *iamRoleAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_iam_role_assignment"
}

func (r *iamRoleAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a CETIC Cloud IAM role to a principal. v1 supports 4 principal " +
			"types: `org_member`, `api_key`, `service_account`, `ccks_workload`. All attributes force " +
			"replacement — no in-place update.\n\n" +
			"~> **Import** — assignments are imported with the composite ID `<role_id>/<assignment_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the assignment.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the role to attach. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"principal_type": schema.StringAttribute{
				MarkdownDescription: "One of: `org_member`, `api_key`, `service_account`, `ccks_workload`. Forces replacement.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("org_member", "api_key", "service_account", "ccks_workload"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"principal_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the principal (e.g. `ccp_service_account.this.id`). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Optional RFC 3339 expiry timestamp. Past expiry, the assignment " +
					"is ignored during policy evaluation. Forces replacement.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *iamRoleAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func setStateFromAPI(m *iamRoleAssignmentResourceModel, p *client.RoleAssignment) {
	m.ID = types.StringValue(p.ID)
	m.RoleID = types.StringValue(p.RoleID)
	m.PrincipalType = types.StringValue(p.PrincipalType)
	m.PrincipalID = types.StringValue(p.PrincipalID)
	if p.ExpiresAt != nil {
		m.ExpiresAt = types.StringValue(p.ExpiresAt.Format(time.RFC3339))
	} else {
		m.ExpiresAt = types.StringNull()
	}
	m.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))
}

func (r *iamRoleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamRoleAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.RoleAssignmentCreateRequest{
		PrincipalType: plan.PrincipalType.ValueString(),
		PrincipalID:   plan.PrincipalID.ValueString(),
	}
	if !plan.ExpiresAt.IsNull() && !plan.ExpiresAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, plan.ExpiresAt.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("expires_at"),
				"Invalid expires_at format",
				fmt.Sprintf("Expected RFC 3339 timestamp, got %q: %v", plan.ExpiresAt.ValueString(), err),
			)
			return
		}
		createReq.ExpiresAt = &t
	}

	created, err := r.client.CreateRoleAssignment(ctx, plan.RoleID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create IAM role assignment", err.Error())
		return
	}

	setStateFromAPI(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *iamRoleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamRoleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetRoleAssignment(ctx, state.RoleID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read IAM role assignment", err.Error())
		return
	}
	setStateFromAPI(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never reached: every attribute carries RequiresReplace.
// Implementing it to satisfy the interface; reaching it means schema drift.
func (r *iamRoleAssignmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported",
		"All IAM role assignment attributes force replacement; reaching Update means schema/impl drift.")
}

func (r *iamRoleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamRoleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteRoleAssignment(ctx, state.RoleID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete IAM role assignment", err.Error())
	}
}

// ImportState parses a composite ID `<role_id>/<assignment_id>` because the
// resource has a 2-tuple primary key.
func (r *iamRoleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Expected format: <role_id>/<assignment_id>
	for i, c := range req.ID {
		if c == '/' {
			roleID, assignmentID := req.ID[:i], req.ID[i+1:]
			if roleID == "" || assignmentID == "" {
				resp.Diagnostics.AddError("Invalid import ID",
					"Expected format `<role_id>/<assignment_id>` (got "+req.ID+")")
				return
			}
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role_id"), roleID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), assignmentID)...)
			return
		}
	}
	resp.Diagnostics.AddError("Invalid import ID",
		"Expected format `<role_id>/<assignment_id>` (got "+req.ID+")")
}
