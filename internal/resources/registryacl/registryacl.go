// Package registryacl implements the ccp_registry_acl Terraform resource.
//
// Each ACL grants a single user a set of `actions` over a repository
// pattern (e.g. `myapp/*`). It's a thin wrapper over one rule in the
// cesanta/docker_auth `acl` list.
//
// Update is fully supported in-place — both `repo_pattern` and `actions`
// are mutable. Only `registry_id` and `user_id` force replacement.
package registryacl

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	_ resource.Resource                = (*registryACLResource)(nil)
	_ resource.ResourceWithConfigure   = (*registryACLResource)(nil)
	_ resource.ResourceWithImportState = (*registryACLResource)(nil)
)

func New() resource.Resource { return &registryACLResource{} }

type registryACLResource struct{ client *client.Client }

type registryACLResourceModel struct {
	ID          types.String `tfsdk:"id"`
	RegistryID  types.String `tfsdk:"registry_id"`
	UserID      types.String `tfsdk:"user_id"`
	Username    types.String `tfsdk:"username"`
	RepoPattern types.String `tfsdk:"repo_pattern"`
	Actions     types.Set    `tfsdk:"actions"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *registryACLResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_registry_acl"
}

// repoPatternRE accepts repo names made of lowercase letters, digits, `-`,
// `_`, `/` and the wildcard `*` — same character set as Docker repo names
// plus glob wildcard for cesanta/docker_auth.
func repoPatternRE() *regexp.Regexp {
	return regexp.MustCompile(`^[a-z0-9_*/-]+$`)
}

func (r *registryACLResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants a `ccp_registry_user` a set of actions over a repository pattern. " +
			"Patterns use shell-style globs as understood by cesanta/docker_auth (e.g. `myapp/*` " +
			"matches all repos starting with `myapp/`, `*` matches any repo). Both `repo_pattern` " +
			"and `actions` are mutable in place.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the ACL rule.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"registry_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_registry`. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the `ccp_registry_user` this ACL applies to. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "Convenience read-back of the user's login name (purely informational).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"repo_pattern": schema.StringAttribute{
				MarkdownDescription: "Repository name pattern this rule matches. Use `*` as wildcard.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(repoPatternRE(),
						"must contain only lowercase letters, digits, '-', '_', '/' or '*'"),
				},
			},
			"actions": schema.SetAttribute{
				MarkdownDescription: "Subset of `pull`, `push`, `*` (admin-equivalent on this pattern).",
				ElementType:         types.StringType,
				Required:            true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf("pull", "push", "*")),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the most recent edit.",
				Computed:            true,
			},
		},
	}
}

func (r *registryACLResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func setACLState(ctx context.Context, m *registryACLResourceModel, p *client.RegistryACL) {
	m.ID = types.StringValue(p.ID)
	m.RegistryID = types.StringValue(p.RegistryID)
	m.UserID = types.StringValue(p.UserID)
	m.Username = types.StringValue(p.Username)
	m.RepoPattern = types.StringValue(p.RepoPattern)
	actions, _ := types.SetValueFrom(ctx, types.StringType, p.Actions)
	m.Actions = actions
	m.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))
	m.UpdatedAt = types.StringValue(p.UpdatedAt.Format(time.RFC3339))
}

func (r *registryACLResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registryACLResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	actions := []string{}
	plan.Actions.ElementsAs(ctx, &actions, false)
	created, err := r.client.CreateRegistryACL(ctx, plan.RegistryID.ValueString(), client.RegistryACLCreateRequest{
		UserID:      plan.UserID.ValueString(),
		RepoPattern: plan.RepoPattern.ValueString(),
		Actions:     actions,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create registry ACL", err.Error())
		return
	}
	setACLState(ctx, &plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *registryACLResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registryACLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	acls, err := r.client.ListRegistryACLs(ctx, state.RegistryID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read registry ACLs", err.Error())
		return
	}
	wantID := state.ID.ValueString()
	for i := range acls {
		if acls[i].ID == wantID {
			setACLState(ctx, &state, &acls[i])
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *registryACLResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state registryACLResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var updReq client.RegistryACLUpdateRequest
	if !plan.RepoPattern.Equal(state.RepoPattern) {
		v := plan.RepoPattern.ValueString()
		updReq.RepoPattern = &v
	}
	if !plan.Actions.Equal(state.Actions) {
		actions := []string{}
		plan.Actions.ElementsAs(ctx, &actions, false)
		updReq.Actions = actions
	}
	updated, err := r.client.UpdateRegistryACL(ctx, state.RegistryID.ValueString(), state.ID.ValueString(), updReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update registry ACL", err.Error())
		return
	}
	setACLState(ctx, &plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *registryACLResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state registryACLResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRegistryACL(ctx, state.RegistryID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete registry ACL", err.Error())
	}
}

// ImportState parses `<registry_id>/<acl_id>`.
func (r *registryACLResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<registry_id>/<acl_id>`, got: "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("registry_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
