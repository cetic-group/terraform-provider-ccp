// Package registry implements the ccp_registry Terraform resource.
//
// CCR (CETIC Container Registry) is a Distribution-based registry deployed
// in a per-tenant K8s namespace within the regional shared workload cluster.
// Tenant-side it has NO network resource (no VPC/VNet/Public IP) — exposure
// is via the 2 shared regional Gateways (`registry-gateway-public` /
// `registry-gateway-private` in `cilium-system`) controlled by the
// `expose_public` / `expose_private` toggles. A single hostname is served
// via DNS split-horizon (public → public Gateway IP, private → private
// Gateway IP).
//
// `admin_password` is returned ONLY by POST /v1/registries — Read() must
// preserve the existing state value (same one-shot pattern as
// ccp_api_key.token).
package registry

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                     = (*registryResource)(nil)
	_ resource.ResourceWithConfigure        = (*registryResource)(nil)
	_ resource.ResourceWithImportState      = (*registryResource)(nil)
	_ resource.ResourceWithValidateConfig   = (*registryResource)(nil)
)

// New returns the resource factory used by `provider.Resources()`.
func New() resource.Resource { return &registryResource{} }

type registryResource struct{ client *client.Client }

type registryResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Slug           types.String `tfsdk:"slug"`
	Region         types.String `tfsdk:"region"`
	ExposePublic   types.Bool   `tfsdk:"expose_public"`
	ExposePrivate  types.Bool   `tfsdk:"expose_private"`
	URL            types.String `tfsdk:"url"`
	ImageTag       types.String `tfsdk:"image_tag"`
	GCScheduleCron types.String `tfsdk:"gc_schedule_cron"`
	Status         types.String `tfsdk:"status"`
	StorageUsedGB  types.Int64  `tfsdk:"storage_used_gb"`
	LastPushAt     types.String `tfsdk:"last_push_at"`
	AdminUsername  types.String `tfsdk:"admin_username"`
	AdminPassword  types.String `tfsdk:"admin_password"`
	Tags           types.List   `tfsdk:"tags"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (r *registryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_registry"
}

func (r *registryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Container Registry (CCR) — a private, OCI-compliant Docker/Helm/OCI " +
			"artifact registry hosted in your tenant. Exposed via the 2 shared regional Cilium Gateways with a " +
			"single split-horizon hostname (`<slug>-<id8>.registry-<region>.cloud.cetic-group.com`). At least one " +
			"of `expose_public` / `expose_private` must be true.\n\n" +
			"~> **`admin_password` is returned only at creation** (one-shot, written to the Terraform state — keep " +
			"your state backend secure). To rotate it, taint the resource. Workload identity for in-cluster pulls " +
			"(CCKS) is handled transparently by the cluster-agent and does not require any Terraform-managed credentials.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the registry.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (1-100 chars).",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"slug": schema.StringAttribute{
				MarkdownDescription: "URL-safe slug derived from `name`, used to build the hostname " +
					"`<slug>-<id8>.registry-<region>.cloud.cetic-group.com`.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code (RNN, PAR, ABJ). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"expose_public": schema.BoolAttribute{
				MarkdownDescription: "Expose the registry on the public Internet. Resolves the hostname to the " +
					"public Gateway IP via the IONOS DNS zone. At least one of `expose_public` / `expose_private` " +
					"must be true.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"expose_private": schema.BoolAttribute{
				MarkdownDescription: "Expose the registry on the CETIC private LAN. Resolves the hostname to the " +
					"private Gateway IP via DNS split-horizon override. At least one of `expose_public` / " +
					"`expose_private` must be true.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "Full HTTPS URL of the registry — same value whether reached over Internet " +
					"or LAN (DNS split-horizon picks the right IP).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"image_tag": schema.StringAttribute{
				MarkdownDescription: "Tag of the upstream `registry` image to deploy. Defaults to the " +
					"backoffice-managed default (currently `2.8`).",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"gc_schedule_cron": schema.StringAttribute{
				MarkdownDescription: "Cron expression (5 fields) for the weekly garbage-collection job " +
					"(server-side, read-only from the provider).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Provisioning status: `creating` | `provisioning` | `active` | `error` | `deleting`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"storage_used_gb": schema.Int64Attribute{
				MarkdownDescription: "Approximate storage used by registry blobs (gigabytes).",
				Computed:            true,
			},
			"last_push_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last push observed by the activity worker.",
				Computed:            true,
			},
			"admin_username": schema.StringAttribute{
				MarkdownDescription: "Username of the auto-provisioned admin user (typically `admin`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"admin_password": schema.StringAttribute{
				MarkdownDescription: "Password of the admin user — returned **only at creation**. " +
					"Stored in the Terraform state. To rotate, `terraform taint` this resource.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form tags (max 60, max 50 chars each).",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateConfig enforces the "at least one exposure mode" invariant at
// plan-time, mirroring the API validator (mig 132 CHECK constraint).
//
// Both `expose_public` and `expose_private` are Optional+Computed with
// `booldefault.StaticBool(...)`. During `terraform validate` (before
// PlanModifiers run), they can surface as Unknown rather than the
// declared defaults. We MUST skip the check when EITHER value is Null
// OR Unknown, otherwise the validator fires spuriously on every
// `terraform validate` invocation of a module/landing-zone consuming
// this resource with default flags. Plan-time enforcement still kicks
// in once values are concretised, and the API CHECK constraint is the
// final source of truth.
func (r *registryResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg registryResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Skip when either side is unresolved — defaults may not have been
	// applied yet at validate-time.
	if cfg.ExposePublic.IsNull() || cfg.ExposePublic.IsUnknown() ||
		cfg.ExposePrivate.IsNull() || cfg.ExposePrivate.IsUnknown() {
		return
	}
	if !cfg.ExposePublic.ValueBool() && !cfg.ExposePrivate.ValueBool() {
		resp.Diagnostics.AddError(
			"At least one exposure must be enabled",
			"`expose_public` and `expose_private` cannot both be false.",
		)
	}
}

func (r *registryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// setState maps the API representation onto the Terraform model.
//
// IMPORTANT — never touch m.AdminPassword here: it's a write-once attribute
// captured by Create() and the API never re-emits it.
func setState(ctx context.Context, m *registryResourceModel, p *client.Registry) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.Slug = types.StringValue(p.Slug)
	m.Region = types.StringValue(p.Region)
	m.ExposePublic = types.BoolValue(p.ExposePublic)
	m.ExposePrivate = types.BoolValue(p.ExposePrivate)
	if p.URL != nil {
		m.URL = types.StringValue(*p.URL)
	} else {
		m.URL = types.StringNull()
	}
	m.ImageTag = types.StringValue(p.ImageTag)
	m.GCScheduleCron = types.StringValue(p.GCScheduleCron)
	m.Status = types.StringValue(p.Status)
	m.CreatedAt = types.StringValue(p.CreatedAt.Format(time.RFC3339))

	if p.StorageUsedGB != nil {
		m.StorageUsedGB = types.Int64Value(*p.StorageUsedGB)
	} else {
		m.StorageUsedGB = types.Int64Null()
	}
	if p.LastPushAt != nil {
		m.LastPushAt = types.StringValue(*p.LastPushAt)
	} else {
		m.LastPushAt = types.StringNull()
	}
	if p.AdminUsername != nil {
		m.AdminUsername = types.StringValue(*p.AdminUsername)
	} else {
		m.AdminUsername = types.StringNull()
	}

	tags, _ := types.ListValueFrom(ctx, types.StringType, p.Tags)
	m.Tags = tags
}

func (r *registryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.RegistryCreateRequest{
		Name:          plan.Name.ValueString(),
		Region:        plan.Region.ValueString(),
		ExposePublic:  plan.ExposePublic.ValueBool(),
		ExposePrivate: plan.ExposePrivate.ValueBool(),
	}
	if !plan.ImageTag.IsNull() && !plan.ImageTag.IsUnknown() {
		v := plan.ImageTag.ValueString()
		createReq.ImageTag = &v
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags := []string{}
		plan.Tags.ElementsAs(ctx, &tags, false)
		createReq.Tags = tags
	}

	created, err := r.client.CreateRegistry(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CETIC Container Registry", err.Error())
		return
	}

	// Capture the one-shot password BEFORE setState() leaves it Unknown.
	adminPassword := created.AdminPassword

	// Poll until provision finishes. Most provisions complete in 1-3 min.
	final, err := pollUntilActive(ctx, r.client, created.ID, 20*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("Registry provisioning timed out or failed", err.Error())
		return
	}

	setState(ctx, &plan, final)
	plan.AdminPassword = types.StringValue(adminPassword)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *registryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetRegistry(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read CETIC Container Registry", err.Error())
		return
	}
	preserved := state.AdminPassword
	setState(ctx, &state, got)
	state.AdminPassword = preserved
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *registryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state registryResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	var updReq client.RegistryUpdateRequest
	patchNeeded := false
	if !plan.ExposePublic.Equal(state.ExposePublic) {
		v := plan.ExposePublic.ValueBool()
		updReq.ExposePublic = &v
		patchNeeded = true
	}
	if !plan.ExposePrivate.Equal(state.ExposePrivate) {
		v := plan.ExposePrivate.ValueBool()
		updReq.ExposePrivate = &v
		patchNeeded = true
	}
	if !plan.Tags.Equal(state.Tags) {
		tags := []string{}
		if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
			plan.Tags.ElementsAs(ctx, &tags, false)
		}
		updReq.Tags = tags
		patchNeeded = true
	}
	if patchNeeded {
		if _, err := r.client.UpdateRegistry(ctx, id, updReq); err != nil {
			resp.Diagnostics.AddError("Failed to update CETIC Container Registry", err.Error())
			return
		}
	}

	final, err := r.client.GetRegistry(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to re-read registry after update", err.Error())
		return
	}
	preserved := state.AdminPassword
	setState(ctx, &plan, final)
	plan.AdminPassword = preserved
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *registryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state registryResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteRegistry(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete CETIC Container Registry", err.Error())
		return
	}
	if err := client.PollUntilDeleted(ctx, 20*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetRegistry(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm CETIC Container Registry deletion", err.Error())
	}
}

func (r *registryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// pollUntilActive blocks until status == active or error, or timeout.
func pollUntilActive(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.Registry, error) {
	deadline := time.Now().Add(timeout)
	for {
		reg, err := c.GetRegistry(ctx, id)
		if err != nil {
			return nil, err
		}
		switch reg.Status {
		case client.RegistryStatusActive:
			return reg, nil
		case client.RegistryStatusError:
			msg := "unknown"
			if reg.ErrorMessage != nil {
				msg = *reg.ErrorMessage
			}
			return reg, fmt.Errorf("registry entered error state: %s", msg)
		}
		if time.Now().After(deadline) {
			return reg, fmt.Errorf("polling timeout (last status: %s)", reg.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}
