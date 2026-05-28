// Package objectstoragekey implements the ccp_object_storage_key resource.
//
// Clé S3 scopée (subuser RGW) pour un tenant CETIC Cloud.
// Les credentials (access_key, secret_key) sont retournés UNIQUEMENT à la
// création — ils sont stockés sensibles dans l'état Terraform.
// Immutable : toute modification force la recréation.
package objectstoragekey

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = (*keyResource)(nil)
	_ resource.ResourceWithConfigure = (*keyResource)(nil)
)

func New() resource.Resource { return &keyResource{} }

type keyResource struct{ client *client.Client }

type keyModel struct {
	ID              types.String `tfsdk:"id"`
	Region          types.String `tfsdk:"region"`
	Label           types.String `tfsdk:"label"`
	AccessLevel     types.String `tfsdk:"access_level"`
	ExpiresInDays   types.Int64  `tfsdk:"expires_in_days"`
	AccessKeyPrefix types.String `tfsdk:"access_key_prefix"`
	AccessKey       types.String `tfsdk:"access_key"`
	SecretKey       types.String `tfsdk:"secret_key"`
	EndpointURL     types.String `tfsdk:"endpoint_url"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

func (r *keyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_object_storage_key"
}

func (r *keyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Clé S3 scopée (subuser RGW Ceph) pour un tenant CETIC Cloud. " +
			"Les credentials ne sont disponibles qu'à la création — les stocker dans Vault ou autre KMS.\n\n" +
			"Immutable — toute modification force la recréation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"region": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"label": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"access_level": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`read` | `write` | `readwrite` | `full`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"expires_in_days": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Durée de validité en jours (1–3650). Omis = pas d'expiration.",
			},
			"access_key_prefix": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Préfixe visible de la clé d'accès (8 premiers caractères).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"access_key": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Clé d'accès S3 complète. Retournée une seule fois à la création.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_key": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Clé secrète S3. Retournée une seule fois à la création.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL S3 endpoint pour cette région.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *keyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
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
	cr := client.ObjectStorageKeyCreateRequest{
		Region:      plan.Region.ValueString(),
		Label:       plan.Label.ValueString(),
		AccessLevel: plan.AccessLevel.ValueString(),
	}
	if !plan.ExpiresInDays.IsNull() && !plan.ExpiresInDays.IsUnknown() {
		v := int(plan.ExpiresInDays.ValueInt64())
		cr.ExpiresInDays = &v
	}
	key, err := r.client.CreateObjectStorageKey(ctx, cr)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create object storage key", err.Error())
		return
	}
	state := keyModel{
		ID:              types.StringValue(key.ID),
		Region:          types.StringValue(key.Region),
		Label:           types.StringValue(key.Label),
		AccessLevel:     types.StringValue(key.AccessLevel),
		ExpiresInDays:   plan.ExpiresInDays,
		AccessKeyPrefix: types.StringValue(key.AccessKeyPrefix),
		AccessKey:       types.StringValue(key.AccessKey),
		SecretKey:       types.StringValue(key.SecretKey),
		EndpointURL:     types.StringValue(key.EndpointURL),
		CreatedAt:       types.StringValue(key.CreatedAt),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *keyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state keyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.GetObjectStorageKey(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read object storage key", err.Error())
		return
	}
	// Preserve sensitive credentials from state — API never returns them again.
	state.AccessKeyPrefix = types.StringValue(key.AccessKeyPrefix)
	state.Region = types.StringValue(key.Region)
	state.Label = types.StringValue(key.Label)
	state.AccessLevel = types.StringValue(key.AccessLevel)
	state.CreatedAt = types.StringValue(key.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *keyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Immutable resource", "Object storage keys are immutable. Use replace.")
}

func (r *keyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state keyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteObjectStorageKey(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete object storage key", err.Error())
		return
	}
}
