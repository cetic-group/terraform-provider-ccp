// Package bucketkey implements the ccp_bucket_key resource.
//
// Clé S3 **scopée à un bucket** (IAM S3 v2, 2026-05-09). Elle remplace
// `ccp_object_storage_key`, dont l'endpoint de création rend désormais
// **410 Gone** : les clés « tenant-wide » ne sont plus émises (#78).
//
// ⚠️ **Le secret n'est rendu qu'UNE FOIS, à la création.** L'API expose bien
// `GET /v1/buckets/{b}/keys/{k}/credentials`, mais il est à **usage unique** :
// il pose `revealed_at` et le second appel rend 410 pour toujours. Ce provider
// ne l'appelle donc JAMAIS — ni en lecture, ni à l'import. Il conserve ce qu'il
// a reçu à la création, et un import laisse `secret_key` nul.
package bucketkey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

var (
	_ resource.Resource                = (*keyResource)(nil)
	_ resource.ResourceWithConfigure   = (*keyResource)(nil)
	_ resource.ResourceWithImportState = (*keyResource)(nil)
)

func New() resource.Resource { return &keyResource{} }

type keyResource struct{ client *client.Client }

type keyModel struct {
	ID              types.String `tfsdk:"id"`
	BucketID        types.String `tfsdk:"bucket_id"`
	Label           types.String `tfsdk:"label"`
	Region          types.String `tfsdk:"region"`
	AccessLevel     types.String `tfsdk:"access_level"`
	ExpiresInDays   types.Int64  `tfsdk:"expires_in_days"`
	AccessKeyPrefix types.String `tfsdk:"access_key_prefix"`
	AccessKey       types.String `tfsdk:"access_key"`
	SecretKey       types.String `tfsdk:"secret_key"`
	EndpointURL     types.String `tfsdk:"endpoint_url"`
	S3BucketName    types.String `tfsdk:"s3_bucket_name"`
	CreatedAt       types.String `tfsdk:"created_at"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	LastUsedAt      types.String `tfsdk:"last_used_at"`
}

func (r *keyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_bucket_key"
}

func (r *keyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "S3 access key **scoped to one bucket**. Replaces " +
			"`ccp_object_storage_key`, whose creation endpoint now returns 410 Gone: " +
			"tenant-wide keys are no longer issued (IAM S3 v2).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"bucket_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the bucket this key is scoped to. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "Human-readable label (1–100 characters). Forces replacement — " +
					"the API has no route to rename a key.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"access_level": schema.StringAttribute{
				MarkdownDescription: "`read`, `write`, `readwrite` (default) or `full`. " +
					"Changed in place — the API exposes a PATCH for this field alone.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("readwrite"),
				Validators: []validator.String{
					// Énumération RÉELLE et fermée côté API
					// (`S3AccessLevelLiteral`), pas un catalogue : la figer ici
					// rend l'erreur au plan plutôt qu'à l'apply, sans jamais
					// bloquer une valeur que l'API accepterait (#71).
					stringvalidator.OneOf("read", "write", "readwrite", "full"),
				},
			},
			"expires_in_days": schema.Int64Attribute{
				MarkdownDescription: "Lifetime in days (1–3650). Omit for a key that never expires. " +
					"Forces replacement — the API only accepts it at creation, and never echoes " +
					"it back: read `expires_at` instead.",
				Optional:      true,
				PlanModifiers: []planmodifier.Int64{int64RequiresReplace{}},
			},
			"access_key_prefix": schema.StringAttribute{Computed: true},
			"access_key": schema.StringAttribute{
				MarkdownDescription: "S3 access key. **Sensitive.**",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_key": schema.StringAttribute{
				MarkdownDescription: "S3 secret key. **Sensitive, returned only once at creation** — " +
					"it cannot be read back, so an imported key leaves it null.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint_url": schema.StringAttribute{
				MarkdownDescription: "S3 endpoint for the region.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"s3_bucket_name": schema.StringAttribute{
				MarkdownDescription: "Bucket name **as S3 sees it** — different from the displayed " +
					"name, and the one external tools expect (Terraform backend, aws cli, boto3).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"region":       schema.StringAttribute{Computed: true},
			"created_at":   schema.StringAttribute{Computed: true},
			"expires_at":   schema.StringAttribute{Computed: true},
			"last_used_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *keyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *keyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan keyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.BucketKeyCreateRequest{
		Label:       plan.Label.ValueString(),
		AccessLevel: plan.AccessLevel.ValueString(),
	}
	if !plan.ExpiresInDays.IsNull() && !plan.ExpiresInDays.IsUnknown() {
		d := int(plan.ExpiresInDays.ValueInt64())
		body.ExpiresInDays = &d
	}

	created, err := r.client.CreateBucketKey(ctx, plan.BucketID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create bucket key", err.Error())
		return
	}

	applyToModel(created, &plan)
	// ⚠️ Les trois secrets ne viennent QUE d'ici. `applyToModel` ne les touche
	// pas, parce qu'il sert aussi à la lecture — où l'API ne les rend jamais et
	// où les écraser les effacerait de l'état.
	plan.AccessKey = types.StringValue(created.AccessKey)
	plan.SecretKey = types.StringValue(created.SecretKey)
	plan.EndpointURL = types.StringValue(created.EndpointURL)
	plan.S3BucketName = types.StringValue(created.S3BucketName)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *keyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state keyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetBucketKey(ctx, state.BucketID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read bucket key", err.Error())
		return
	}

	// ⚠️ **Une clé révoquée répond encore 200.** La suppression est en deux
	// temps côté API : le premier DELETE pose `revoked_at`, le second efface la
	// ligne. Sans ce contrôle, une révocation faite hors de Terraform passerait
	// inaperçue et l'état continuerait d'annoncer une clé qui n'ouvre plus rien.
	if got.RevokedAt != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	applyToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *keyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state keyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.UpdateBucketKey(ctx, plan.BucketID.ValueString(), state.ID.ValueString(),
		client.BucketKeyPatchRequest{AccessLevel: plan.AccessLevel.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update bucket key", err.Error())
		return
	}

	applyToModel(got, &plan)
	// Les secrets ne sont pas re-rendus par le PATCH : on garde l'état.
	plan.AccessKey = state.AccessKey
	plan.SecretKey = state.SecretKey
	plan.EndpointURL = state.EndpointURL
	plan.S3BucketName = state.S3BucketName

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *keyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state keyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteBucketKey(ctx, state.BucketID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to revoke bucket key", err.Error())
	}
}

// ImportState prend `<bucket_id>/<key_id>` : l'API scope la clé sous son
// bucket, donc l'identifiant seul ne suffit pas à la retrouver.
//
// ⚠️ `secret_key` reste NUL après un import, et c'est irrémédiable : la
// révélation est à usage unique côté API. Une clé importée sert à gérer son
// cycle de vie, pas à récupérer son secret.
func (r *keyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			fmt.Sprintf("Expected `<bucket_id>/<key_id>`, got %q. A bucket key is "+
				"scoped to its bucket, so the key id alone does not identify it.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("bucket_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	resp.Diagnostics.AddWarning(
		"Secret key cannot be imported",
		"`secret_key` stays null on an imported key: the API returns it only once, "+
			"at creation, and its reveal endpoint is single-use. Revoke and recreate "+
			"the key if you need the secret.",
	)
}

// applyToModel projette la réponse de l'API. Il ne touche JAMAIS aux quatre
// champs que seule la création rend : les écraser avec des valeurs vides les
// effacerait de l'état à la première lecture.
func applyToModel(src *client.BucketKey, dst *keyModel) {
	dst.ID = types.StringValue(src.ID)
	dst.BucketID = types.StringValue(src.BucketID)
	dst.Label = types.StringValue(src.Label)
	dst.Region = types.StringValue(src.Region)
	dst.AccessLevel = types.StringValue(src.AccessLevel)
	dst.AccessKeyPrefix = types.StringValue(src.AccessKeyPrefix)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))
	dst.ExpiresAt = timePtr(src.ExpiresAt)
	dst.LastUsedAt = timePtr(src.LastUsedAt)
}

func timePtr(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}

// int64RequiresReplace : le pendant Int64 de `stringplanmodifier.RequiresReplace`.
type int64RequiresReplace struct{}

func (int64RequiresReplace) Description(context.Context) string { return "Forces replacement." }
func (int64RequiresReplace) MarkdownDescription(context.Context) string {
	return "Forces replacement."
}
func (int64RequiresReplace) PlanModifyInt64(_ context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	if !req.StateValue.Equal(req.PlanValue) {
		resp.RequiresReplace = true
	}
}
