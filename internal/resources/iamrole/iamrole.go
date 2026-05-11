// Package iamrole implements the ccp_iam_role Terraform resource (IAM Roles v1).
//
// A role groups one or more policy statements (Allow/Deny on action × resource
// ARN) that can be assigned to principals (org members, API keys, service
// accounts, CCKS workloads) via `ccp_iam_role_assignment`. Custom tenant-scoped
// roles only — the 10 platform built-ins are seeded server-side and exposed
// via the `ccp_iam_role` data source.
//
// `policy_document_json` is a raw JSON string (single attribute, JSON-encoded
// PolicyDocument). The provider passes it through to the API and uses the
// `JSONNormalizeEqual` plan modifier to suppress spurious diffs when the API
// re-serializes via JCS canonicalisation in a different key order than the
// user wrote.
package iamrole

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	ccppm "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/planmodifier"
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
	_ resource.Resource                = (*iamRoleResource)(nil)
	_ resource.ResourceWithConfigure   = (*iamRoleResource)(nil)
	_ resource.ResourceWithImportState = (*iamRoleResource)(nil)
)

// New returns the resource factory used by `provider.Resources()`.
func New() resource.Resource { return &iamRoleResource{} }

type iamRoleResource struct{ client *client.Client }

type iamRoleResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	PolicyDocumentJSON types.String `tfsdk:"policy_document_json"`
	IsBuiltIn          types.Bool   `tfsdk:"is_built_in"`
	PolicyHash         types.String `tfsdk:"policy_hash"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func (r *iamRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_role"
}

func (r *iamRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a custom CETIC Cloud IAM role (Roles v1). A role is a named " +
			"bundle of policy statements (Allow/Deny on (action, resource ARN)) that can be assigned " +
			"to principals (org members, API keys, service accounts, CCKS workloads) via " +
			"`ccp_iam_role_assignment`. The 10 platform built-in roles (`AdminAll`, `ReadOnlyAll`, " +
			"`Member`, `RegistryAdmin`, `RegistryReader`, `BucketReader`, `BucketWriter`, `K8sViewer`, " +
			"`BillingReader`, `NetworkAdmin`) are seeded server-side and not manageable through this " +
			"resource — use the `ccp_iam_role` data source to look them up.\n\n" +
			"~> **Policy document layout** — the API canonicalises the document using a JCS " +
			"RFC 8785-equivalent algorithm and may return keys in a different order than your input. " +
			"The provider uses a `JSONNormalizeEqual` plan modifier on `policy_document_json` to " +
			"suppress spurious diffs when state and plan are semantically equivalent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the role.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (1-64 chars). Must be unique within the tenant.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 64)},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Free-form description (max 512 chars).",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtMost(512)},
			},
			"policy_document_json": schema.StringAttribute{
				MarkdownDescription: "PolicyDocument as a JSON string (AWS IAM-style — version + statements). " +
					"Use the `ccp_iam_policy_document` data source to author it ergonomically. The API " +
					"canonicalises and computes a SHA-256 hash (`policy_hash`).",
				Required: true,
				PlanModifiers: []planmodifier.String{
					ccppm.JSONNormalizeEqual(),
				},
			},
			"is_built_in": schema.BoolAttribute{
				MarkdownDescription: "Always `false` for resources managed via Terraform. Built-in roles " +
					"are read-only and managed server-side via the seed migration.",
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{},
			},
			"policy_hash": schema.StringAttribute{
				MarkdownDescription: "SHA-256 hex of the canonicalised PolicyDocument — useful for drift " +
					"detection (e.g. checking that two roles encode the same policy).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last update.",
				Computed:            true,
			},
		},
	}
}

func (r *iamRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// setStateFromAPI maps an API Role onto the model. policy_document_json is
// overwritten with the canonical JSON returned by the API, but the plan
// modifier JSONNormalizeEqual will preserve the state value when they are
// semantically equal — preventing perma-diffs from key reordering.
func setStateFromAPI(m *iamRoleResourceModel, p *client.Role) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	if p.Description != nil {
		m.Description = types.StringValue(*p.Description)
	} else {
		m.Description = types.StringNull()
	}
	m.PolicyDocumentJSON = types.StringValue(string(p.PolicyDocument))
	m.IsBuiltIn = types.BoolValue(p.IsBuiltIn)
	m.PolicyHash = types.StringValue(p.PolicyHash)
	m.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))
	m.UpdatedAt = types.StringValue(p.UpdatedAt.Format(time.RFC3339))
}

func (r *iamRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan iamRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.RoleCreateRequest{
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		createReq.Description = &v
	}
	// policy_document is a raw JSON message — preserve user's bytes
	createReq.PolicyDocument = json.RawMessage(plan.PolicyDocumentJSON.ValueString())

	created, err := r.client.CreateRole(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CETIC Cloud IAM role", err.Error())
		return
	}

	setStateFromAPI(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *iamRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state iamRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetRole(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read CETIC Cloud IAM role", err.Error())
		return
	}
	setStateFromAPI(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *iamRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state iamRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	var updReq client.RoleUpdateRequest
	patchNeeded := false

	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		updReq.Name = &v
		patchNeeded = true
	}
	if !plan.Description.Equal(state.Description) {
		// Send empty-string when user removes description (Pydantic accepts).
		v := ""
		if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
			v = plan.Description.ValueString()
		}
		updReq.Description = &v
		patchNeeded = true
	}
	if !plan.PolicyDocumentJSON.Equal(state.PolicyDocumentJSON) {
		updReq.PolicyDocument = json.RawMessage(plan.PolicyDocumentJSON.ValueString())
		patchNeeded = true
	}

	if patchNeeded {
		if _, err := r.client.UpdateRole(ctx, id, updReq); err != nil {
			resp.Diagnostics.AddError("Failed to update CETIC Cloud IAM role", err.Error())
			return
		}
	}

	final, err := r.client.GetRole(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to re-read IAM role after update", err.Error())
		return
	}
	setStateFromAPI(&plan, final)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *iamRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state iamRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteRole(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete CETIC Cloud IAM role", err.Error())
	}
}

func (r *iamRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
