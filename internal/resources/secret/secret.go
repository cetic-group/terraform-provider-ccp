// Package secret implements the ccp_secret Terraform resource (Secret
// Manager v1).
//
// A secret is an encrypted key/value blob stored in the CETIC Cloud Secret
// Manager. It is intentionally K8s-agnostic: the Secret resource is a
// generic vault entry. When projected into a Kubernetes cluster via the
// `CCPSecret` CRD, the Kubernetes Secret type is specified at the CRD
// level on the workload cluster — not here.
//
// The plaintext `data` map is persisted in the Terraform state
// (`Sensitive`) and never re-fetched from the API: the dedicated reveal
// endpoint `/v1/secrets/{id}/value` is audit-logged and rate-limited, so
// calling it on every `terraform refresh` would be both noisy and costly.
// As a side effect, drift on `data` outside of Terraform is NOT detected;
// drift on metadata (`description`, `tags`, `version`) IS detected
// through the regular GET endpoint.
//
// CRUD semantics :
//   - Create : POST /v1/secrets — sends the full payload, stores returned
//     metadata + the plan-side `data`.
//   - Read   : GET /v1/secrets/{id} — refreshes metadata only. 404 ⇒
//     removed from state.
//   - Update : PATCH for description/tags, POST .../rotate for `data`.
//     When both change in a single plan, rotate happens first (cleaner
//     audit log: version-bump preceeds metadata edit).
//   - Delete : DELETE /v1/secrets/{id}. 404 ⇒ idempotent no-op.
//
// `name` is immutable — modifying it forces a replace (the API itself
// rejects the change with 422; we surface it as a RequiresReplace plan
// modifier for cleaner UX).
package secret

import (
	"context"
	"fmt"
	"regexp"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*secretResource)(nil)
	_ resource.ResourceWithConfigure   = (*secretResource)(nil)
	_ resource.ResourceWithImportState = (*secretResource)(nil)
)

// nameRegex matches the path-based (Vault KV style) secret name accepted by
// the API (mirrored from `apps/api/app/schemas/secret.py::NAME_REGEX`).
// One or more DNS-friendly segments joined by `/`. Examples:
//   "password", "prod/db/credentials", "team-a/api-tokens/github".
var nameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}(/[a-z][a-z0-9-]{0,62})*$`)

// New returns the resource factory used by `provider.Resources()`.
func New() resource.Resource { return &secretResource{} }

type secretResource struct{ client *client.Client }

type secretResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Data        types.Map    `tfsdk:"data"`
	Tags        types.List   `tfsdk:"tags"`
	Version     types.Int64  `tfsdk:"version"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud secret — an encrypted key/value blob stored in the " +
			"Secret Manager. The Secret is K8s-agnostic: it holds a generic `string → string` map. " +
			"When projected into a Kubernetes cluster via the `CCPSecret` CRD, the native Kubernetes " +
			"Secret type (`Opaque`, `kubernetes.io/tls`, …) is specified on the CRD at the workload " +
			"cluster — not on this resource.\n\n" +
			"~> **`data` is Sensitive.** Plaintext values are persisted in the Terraform state — keep " +
			"your state backend secure (encrypted at rest, restricted access).\n\n" +
			"~> **Drift on `data` is NOT detected.** The provider does not call the audit-logged reveal " +
			"endpoint on every refresh; it keeps the plaintext from the most recent Create / Update. " +
			"To force-resync after an out-of-band rotation, taint the resource or change `data` in the " +
			"config.\n\n" +
			"~> **`name` is immutable.** Changing it forces a destroy + create.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the secret.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "DNS-friendly secret name, unique within the org. Matches " +
					"`^[a-z][a-z0-9-]{0,62}$`. **Immutable** — changing forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(nameRegex,
						"must match ^[a-z][a-z0-9-]{0,62}$ (DNS-friendly)"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Free-form description (max 500 chars). Mutable in place.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(500),
				},
			},
			"data": schema.MapAttribute{
				MarkdownDescription: "Map of plaintext key/value pairs to encrypt. Persisted Sensitive " +
					"in the Terraform state. Changing `data` triggers a server-side rotation (the API " +
					"endpoint `POST /v1/secrets/{id}/rotate` is invoked) and bumps `version`.",
				ElementType: types.StringType,
				Required:    true,
				Sensitive:   true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form tags attached to the secret. Mutable in place via PATCH.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "Server-side monotonic version counter. Bumped each time `data` is " +
					"rotated.",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last metadata or rotation update.",
				Computed:            true,
			},
		},
	}
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// applySecretToModel maps an API Secret onto the model — WITHOUT touching
// state.Data. Callers must pose Data themselves (Create / Update set it
// from the plan; Read preserves the state value). Tags are normalised so
// a `nil` API response and an empty list both produce an empty list in
// state (avoids spurious diffs against an Optional+Computed list).
func applySecretToModel(ctx context.Context, m *secretResourceModel, s *client.Secret) diag.Diagnostics {
	m.ID = types.StringValue(s.ID)
	m.Name = types.StringValue(s.Name)
	if s.Description != nil {
		m.Description = types.StringValue(*s.Description)
	} else {
		m.Description = types.StringNull()
	}
	m.Version = types.Int64Value(s.Version)

	tagValues := make([]string, 0, len(s.Tags))
	tagValues = append(tagValues, s.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	m.Tags = tagsList

	m.CreatedAt = types.StringValue(s.CreatedAt)
	m.UpdatedAt = types.StringValue(s.UpdatedAt)
	return nil
}

// mapStringValues extracts a `map[string]string` from a `types.Map` of
// StringType elements. Returns an empty map when the attribute is null or
// unknown.
func mapStringValues(ctx context.Context, m types.Map) (map[string]string, error) {
	out := map[string]string{}
	if m.IsNull() || m.IsUnknown() {
		return out, nil
	}
	diags := m.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		return out, fmt.Errorf("decode map: %v", diags)
	}
	return out, nil
}

// stringsFromList converts the framework List representation into a Go
// slice. Null and unknown both collapse to nil so callers can hand the
// result straight to the API client.
func stringsFromList(ctx context.Context, list types.List) ([]string, error) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	out := make([]string, 0, len(list.Elements()))
	diags := list.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		return nil, fmt.Errorf("decode list: %v", diags)
	}
	return out, nil
}

func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data, err := mapStringValues(ctx, plan.Data)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("data"), "Invalid data map", err.Error())
		return
	}
	tags, err := stringsFromList(ctx, plan.Tags)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("tags"), "Invalid tags list", err.Error())
		return
	}
	if tags == nil {
		tags = []string{}
	}

	payload := client.SecretCreatePayload{
		Name: plan.Name.ValueString(),
		Data: data,
		Tags: tags,
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		payload.Description = &v
	}

	created, err := r.client.CreateSecret(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CETIC Cloud secret", err.Error())
		return
	}

	// IMPORTANT — capture the plan's `data` BEFORE applySecretToModel: the
	// API response does NOT echo `data` back (security-by-design), so we
	// rely on the plan value. Same idiom as ccp_service_account.token
	// preservation in serviceaccount.Create.
	planData := plan.Data
	diags := applySecretToModel(ctx, &plan, created)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Data = planData

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetSecret(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read secret", err.Error())
		return
	}

	// Preserve state.Data — the read endpoint never returns plaintext, and
	// we deliberately skip the audit-logged reveal endpoint here. Drift on
	// `data` is therefore not detected (documented in the Schema).
	stateData := state.Data
	diags := applySecretToModel(ctx, &state, got)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Data = stateData

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Belt-and-suspenders: name carries a RequiresReplace plan modifier, so
	// Update() should never see it change. Surface a loud error if drift
	// slipped through (schema/impl mismatch).
	if !plan.Name.Equal(state.Name) {
		resp.Diagnostics.AddAttributeError(path.Root("name"),
			"name is immutable",
			"Changing `name` should have triggered a replace, not an update — please file a bug.")
		return
	}

	dataChanged := !plan.Data.Equal(state.Data)
	descChanged := !plan.Description.Equal(state.Description)
	tagsChanged := !plan.Tags.Equal(state.Tags)

	// 1. Rotate first — bumps version and writes an audit-log row BEFORE
	//    metadata edits, which keeps the audit trail clean (version-bump
	//    precedes description/tags edits in time).
	if dataChanged {
		data, err := mapStringValues(ctx, plan.Data)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("data"), "Invalid data map", err.Error())
			return
		}
		if _, err := r.client.RotateSecret(ctx, id, client.SecretRotatePayload{Data: data}); err != nil {
			resp.Diagnostics.AddError("Failed to rotate secret data", err.Error())
			return
		}
	}

	// 2. Then PATCH metadata if needed.
	if descChanged || tagsChanged {
		var upd client.SecretUpdatePayload
		if descChanged {
			v := ""
			if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
				v = plan.Description.ValueString()
			}
			upd.Description = &v
		}
		if tagsChanged {
			tags, err := stringsFromList(ctx, plan.Tags)
			if err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("tags"), "Invalid tags list", err.Error())
				return
			}
			// JSON encoding via the `*[]string` pointer means nil slice ⇒ `null`
			// in the payload. The API accepts an empty list to clear tags, so
			// promote nil to an empty slice for consistency with Create.
			if tags == nil {
				tags = []string{}
			}
			upd.Tags = &tags
		}
		if _, err := r.client.UpdateSecret(ctx, id, upd); err != nil {
			resp.Diagnostics.AddError("Failed to update secret metadata", err.Error())
			return
		}
	}

	// Re-read to get the final canonical metadata (version bumped, updated_at).
	final, err := r.client.GetSecret(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to re-read secret after update", err.Error())
		return
	}

	// Preserve plan.Data — the GET response does not include plaintext.
	planData := plan.Data
	diags := applySecretToModel(ctx, &plan, final)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.Data = planData

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSecret(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete secret", err.Error())
	}
}

// ImportState rebinds an existing secret by UUID. The `data` attribute
// will be NULL after import — the API does not re-emit plaintext outside
// of the audit-logged reveal endpoint, and the provider chooses not to
// call it implicitly. Run a `terraform apply` after import with the
// expected `data` to reconcile.
func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
