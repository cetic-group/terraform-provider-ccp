// Package registryuser implements the ccp_registry_user Terraform resource.
//
// Registry users are non-admin credentials provisioned through cesanta
// docker_auth — used by CI pipelines and human contributors who need
// scoped pull/push access. The admin user is auto-created with the
// `ccp_registry` resource and not exposed here.
//
// `password` is delivered ONCE by POST /v1/registries/{id}/users and never
// again. It is captured into state at Create() and explicitly preserved
// during Read() (same pattern as ccp_api_key.token / ccp_registry.admin_password).
//
// There is no Update — `username` is RequiresReplace and that's the only
// mutable field. To rotate a password, taint the resource: Terraform will
// destroy + re-create which re-issues a fresh password.
package registryuser

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
	_ resource.Resource                = (*registryUserResource)(nil)
	_ resource.ResourceWithConfigure   = (*registryUserResource)(nil)
	_ resource.ResourceWithImportState = (*registryUserResource)(nil)
)

func New() resource.Resource { return &registryUserResource{} }

type registryUserResource struct{ client *client.Client }

type registryUserResourceModel struct {
	ID         types.String `tfsdk:"id"`
	RegistryID types.String `tfsdk:"registry_id"`
	Username   types.String `tfsdk:"username"`
	IsAdmin    types.Bool   `tfsdk:"is_admin"`
	Password   types.String `tfsdk:"password"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (r *registryUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_user"
}

func (r *registryUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a non-admin user of a CETIC Container Registry. The user logs in " +
			"with `docker login <registry hostname>` using `username` and the one-shot `password`. " +
			"Use `ccp_registry_acl` to grant pull/push permissions on repository patterns.\n\n" +
			"~> **`password` is returned only at creation** and never re-emitted by the API. To rotate it, " +
			"`terraform taint` this resource: the destroy + create cycle issues a new password.\n\n" +
			"All attributes force replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the registry user.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"registry_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_registry`. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Login username. Lower-case letters, digits, and `-` (1-32 chars). " +
					"Must be unique within the registry. Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 32),
					stringvalidator.RegexMatches(usernamePattern(), "must contain only lowercase letters, digits and -"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"is_admin": schema.BoolAttribute{
				MarkdownDescription: "Whether this user is the auto-provisioned registry admin. Always " +
					"`false` for users created via this resource (admin is owned by `ccp_registry`).",
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Generated password — returned **only at creation**. Stored in the " +
					"Terraform state. To rotate, taint the resource.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *registryUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *registryUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registryUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateRegistryUser(ctx, plan.RegistryID.ValueString(), client.RegistryUserCreateRequest{
		Username: plan.Username.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create registry user", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ID)
	plan.RegistryID = types.StringValue(created.RegistryID)
	plan.Username = types.StringValue(created.Username)
	plan.IsAdmin = types.BoolValue(created.IsAdmin)
	plan.Password = types.StringValue(created.Password)
	plan.CreatedAt = types.StringValue(created.CreatedAt.Format(time.RFC3339))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *registryUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registryUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	users, err := r.client.ListRegistryUsers(ctx, state.RegistryID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Parent registry is gone — drop the user from state too.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read registry user", err.Error())
		return
	}

	var found *client.RegistryUser
	wantID := state.ID.ValueString()
	for i := range users {
		if users[i].ID == wantID {
			found = &users[i]
			break
		}
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// IMPORTANT — never touch state.Password here. The API never re-emits
	// it; we keep the Create()-captured value as-is. Same idiom as
	// apikey.Read() preserving state.Token.
	state.RegistryID = types.StringValue(found.RegistryID)
	state.Username = types.StringValue(found.Username)
	state.IsAdmin = types.BoolValue(found.IsAdmin)
	state.CreatedAt = types.StringValue(found.CreatedAt.Format(time.RFC3339))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *registryUserResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported",
		"All ccp_registry_user attributes force replacement; reaching Update means schema/impl drift.")
}

func (r *registryUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state registryUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRegistryUser(ctx, state.RegistryID.ValueString(), state.Username.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete registry user", err.Error())
	}
}

// ImportState parses `<registry_id>/<user_id>` so users can rebind a
// pre-existing user into Terraform.
func (r *registryUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<registry_id>/<user_id>`, got: "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("registry_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
