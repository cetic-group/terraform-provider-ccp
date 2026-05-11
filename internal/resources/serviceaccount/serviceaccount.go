// Package serviceaccount implements the ccp_service_account Terraform resource.
//
// A service account is a machine identity (token-based, prefix `ccp_sa_`) used
// to attach IAM roles to a non-human caller — e.g. a CI pipeline or a
// long-running automation job. Service accounts are scoped to the calling
// org and authenticate by Bearer token like API keys, but their permissions
// come **only** from IAM role assignments (no static `scope` like
// `read/write/admin` for API keys).
//
// `token` is delivered ONCE by POST /v1/service-accounts and never again.
// It is captured into state at Create() and explicitly preserved during
// Read() (same pattern as ccp_api_key.token / ccp_registry_user.password).
//
// `name`, `description` and `expires_at` are mutable in place via PATCH.
// To rotate the token, taint the resource: the destroy + create cycle
// issues a fresh token. (Native rotation is exposed by the API on
// `POST /v1/service-accounts/{id}/rotate` but Terraform's declarative
// model maps poorly to it — taint or `terraform apply -replace` are the
// idiomatic answer.)
package serviceaccount

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
	_ resource.Resource                = (*serviceAccountResource)(nil)
	_ resource.ResourceWithConfigure   = (*serviceAccountResource)(nil)
	_ resource.ResourceWithImportState = (*serviceAccountResource)(nil)
)

// New returns the resource factory used by `provider.Resources()`.
func New() resource.Resource { return &serviceAccountResource{} }

type serviceAccountResource struct{ client *client.Client }

type serviceAccountResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
	Token       types.String `tfsdk:"token"`
	TokenPrefix types.String `tfsdk:"token_prefix"`
	LastUsedAt  types.String `tfsdk:"last_used_at"`
	RotatedAt   types.String `tfsdk:"rotated_at"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *serviceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (r *serviceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud service account — a machine identity that authenticates " +
			"with a `ccp_sa_*` token and derives permissions exclusively from IAM role assignments " +
			"(`ccp_iam_role_assignment` with `principal_type = \"service_account\"`).\n\n" +
			"~> **`token` is returned only at creation** and never re-emitted by the API. It is written " +
			"to the Terraform state and is `Sensitive`. To rotate it, `terraform taint` this resource: " +
			"the destroy + create cycle issues a fresh token.\n\n" +
			"`name`, `description` and `expires_at` are mutable in place — Terraform will issue a PATCH " +
			"rather than a replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the service account.",
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
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "Optional RFC 3339 timestamp after which the service account token is " +
					"rejected by the API.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "Full service account token (`ccp_sa_<43 chars>`). Returned **only at " +
					"creation**, never re-emitted by the API. Persisted in the Terraform state — keep " +
					"your state backend secure.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"token_prefix": schema.StringAttribute{
				MarkdownDescription: "Visible token prefix (e.g. `ccp_sa_xxxxxxxx`) used for identification. " +
					"Safe to log.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_used_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last request authenticated with this token.",
				Computed:            true,
			},
			"rotated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last token rotation (server-side). Always null " +
					"for SAs managed via Terraform — rotation through this resource happens via taint, " +
					"which destroys and re-creates the SA from scratch.",
				Computed: true,
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

func (r *serviceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// applySAToModel maps an API ServiceAccount onto the model — WITHOUT touching
// state.Token. Callers must pose Token themselves (Create reveals it once;
// Read preserves the state value).
func applySAToModel(m *serviceAccountResourceModel, p *client.ServiceAccount) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	if p.Description != nil {
		m.Description = types.StringValue(*p.Description)
	} else {
		m.Description = types.StringNull()
	}
	if p.ExpiresAt != nil {
		m.ExpiresAt = types.StringValue(p.ExpiresAt.Format(time.RFC3339))
	} else {
		m.ExpiresAt = types.StringNull()
	}
	m.TokenPrefix = types.StringValue(p.TokenPrefix)
	if p.LastUsedAt != nil {
		m.LastUsedAt = types.StringValue(p.LastUsedAt.Format(time.RFC3339))
	} else {
		m.LastUsedAt = types.StringNull()
	}
	if p.RotatedAt != nil {
		m.RotatedAt = types.StringValue(p.RotatedAt.Format(time.RFC3339))
	} else {
		m.RotatedAt = types.StringNull()
	}
	m.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))
	// API model doesn't carry UpdatedAt; the server uses CreatedAt as a
	// fallback for the read path. Mirror that here so the schema stays
	// satisfied; PATCH responses overwrite to a fresh value.
	m.UpdatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))
}

func (r *serviceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serviceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.ServiceAccountCreateRequest{
		Name: plan.Name.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		createReq.Description = &v
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
		// Convert to days (rounded up) for the API. The contract uses
		// `expires_in_days` server-side; we receive `expires_at` here for
		// HCL ergonomics.
		days := int(time.Until(t).Hours()/24) + 1
		if days < 1 {
			days = 1
		}
		createReq.ExpiresInDays = &days
	}

	created, err := r.client.CreateServiceAccount(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CETIC Cloud service account", err.Error())
		return
	}

	// IMPORTANT — capture token BEFORE any setState(): the API returns
	// it ONCE, on this response. Subsequent Read() calls never re-emit it
	// (cf. piège plugin-framework #3 in plugin_framework_pitfalls.md).
	token := created.Token

	applySAToModel(&plan, &created.ServiceAccount)
	plan.Token = types.StringValue(token)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serviceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetServiceAccount(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read service account", err.Error())
		return
	}

	// IMPORTANT — never overwrite state.Token here. The API never re-emits
	// it; we keep the Create()-captured value as-is. Same idiom as
	// apikey.Read() preserving state.Token and registryuser.Read()
	// preserving state.Password.
	applySAToModel(&state, got)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *serviceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serviceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	var updReq client.ServiceAccountUpdateRequest
	patchNeeded := false

	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		updReq.Name = &v
		patchNeeded = true
	}
	if !plan.Description.Equal(state.Description) {
		v := ""
		if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
			v = plan.Description.ValueString()
		}
		updReq.Description = &v
		patchNeeded = true
	}
	// expires_at is intentionally not propagated here — the API does not
	// expose a PATCH path for it. Users who need to change expiry should
	// taint the resource (which forces re-creation and issues a fresh
	// token anyway).

	if patchNeeded {
		if _, err := r.client.UpdateServiceAccount(ctx, id, updReq); err != nil {
			resp.Diagnostics.AddError("Failed to update service account", err.Error())
			return
		}
	}

	final, err := r.client.GetServiceAccount(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to re-read service account after update", err.Error())
		return
	}

	// Preserve token from state across the update — same reasoning as Read.
	token := state.Token
	applySAToModel(&plan, final)
	plan.Token = token

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serviceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteServiceAccount(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete service account", err.Error())
	}
}

// ImportState rebinds an existing service account by UUID. The `token`
// attribute will be NULL after import — the API does not re-emit it.
func (r *serviceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
