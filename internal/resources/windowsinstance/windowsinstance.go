// Package windowsinstance implements the ccp_windows_instance Terraform resource.
//
// A Windows instance in CETIC Cloud is a managed Windows VM provisioned via the
// dockur stack. Unlike the QEMU `ccp_vm_instance`, the CETIC Cloud Windows API
// has NO PATCH endpoint: there is no in-place mutation path, so EVERY
// user-settable attribute forces replacement on change.
//
// CETIC Cloud provides NO Windows license: the caller is responsible for
// holding a valid license per instance and must opt in by setting
// `license_consent = true`. The resource's ModifyPlan rejects an explicit
// `false` early (at plan time) before any API call.
//
// Provisioning is asynchronous: POST /v1/windows-instances returns 201 with an
// in-flight status (installing/provisioning) and a worker performs the Windows
// install + first boot. We poll GetWindowsInstance for up to 20 minutes
// (Windows installs are slow) until the instance reaches `running`. If it
// reports `error`, we fail with the API-side error_message.
//
// Deletion is also asynchronous: the instance enters `deleting` and disappears
// from the API once teardown completes. We poll for 404 up to 10 minutes and
// surface a warning rather than an error if the timeout elapses.
package windowsinstance

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*windowsInstanceResource)(nil)
	_ resource.ResourceWithConfigure   = (*windowsInstanceResource)(nil)
	_ resource.ResourceWithImportState = (*windowsInstanceResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*windowsInstanceResource)(nil)
)

// New returns a freshly-constructed ccp_windows_instance resource.
// Wired in by provider.go via windowsinstance.New.
func New() resource.Resource {
	return &windowsInstanceResource{}
}

// windowsInstanceResource is the framework Resource implementation. The client
// is stashed in Configure and reused by Create/Read/Update/Delete.
type windowsInstanceResource struct {
	client *client.Client
}

// windowsInstanceResourceModel mirrors the schema below 1-to-1. Tag names must
// match the schema attribute keys exactly.
type windowsInstanceResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Region                types.String `tfsdk:"region"`
	Template              types.String `tfsdk:"template"`
	Plan                  types.String `tfsdk:"plan"`
	AdministratorPassword types.String `tfsdk:"administrator_password"`
	VnetID                types.String `tfsdk:"vnet_id"`
	PublicIPID            types.String `tfsdk:"public_ip_id"`
	DataVolumeIDs         types.List   `tfsdk:"data_volume_ids"`
	Tags                  types.List   `tfsdk:"tags"`
	LicenseConsent        types.Bool   `tfsdk:"license_consent"`
	Hostname              types.String `tfsdk:"hostname"`
	Cores                 types.Int64  `tfsdk:"cores"`
	MemoryMB              types.Int64  `tfsdk:"memory_mb"`
	DiskGB                types.Int64  `tfsdk:"disk_gb"`
	Status                types.String `tfsdk:"status"`
	IPAddress             types.String `tfsdk:"ip_address"`
	PublicIPAddress       types.String `tfsdk:"public_ip_address"`
	HasAdminPassword      types.Bool   `tfsdk:"has_admin_password"`
	ErrorMessage          types.String `tfsdk:"error_message"`
	CreatedAt             types.String `tfsdk:"created_at"`
	UpdatedAt             types.String `tfsdk:"updated_at"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscores
// and hyphens, 1..100 chars.
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,100}$`)

// uuidPattern is a permissive RFC 4122 matcher for CETIC Cloud resource IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Polling parameters.
//   - Create waits up to 20 minutes for the instance to reach `running`.
//     Windows installs are slow (image extraction + setup + first boot).
//   - Delete waits up to 10 minutes for the instance to disappear (404).
const (
	createPollInterval = 10 * time.Second
	createPollTimeout  = 20 * time.Minute
	deletePollInterval = 5 * time.Second
	deletePollTimeout  = 10 * time.Minute
)

func (r *windowsInstanceResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_windows_instance"
}

func (r *windowsInstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud Windows instance (provisioned via dockur). " +
			"The Windows API has no in-place update endpoint, so **every** user-settable " +
			"attribute forces replacement on change. Creation is asynchronous: the provider " +
			"polls until the instance reaches the `running` state (or up to 20 minutes). " +
			"**CETIC Cloud provides no Windows license** — you must hold a valid license for " +
			"each instance and opt in with `license_consent = true`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the Windows instance.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Instance name (1–100 chars; alphanumerics, `_`, and `-`). " +
					"Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must be 1–100 chars containing only letters, digits, underscores, or hyphens",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "CETIC Cloud region. One of `RNN` (Rennes, France), " +
					"`PAR` (Paris, France), or `ABJ` (Abidjan, Côte d'Ivoire). Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("RNN", "PAR", "ABJ"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"template": schema.StringAttribute{
				MarkdownDescription: "Windows template key (e.g. `windows-2022`, `windows-11`). " +
					"Must match an active template registered in the CETIC Cloud catalogue. " +
					"Forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Compute plan: `nano`, `micro`, `small`, `medium`, " +
					"`large`, or `xlarge`. Each plan maps to a fixed (cores, memory, disk) " +
					"tuple. Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("nano", "micro", "small", "medium", "large", "xlarge"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"administrator_password": schema.StringAttribute{
				MarkdownDescription: "Administrator password set during install (8–128 chars). " +
					"Sensitive: never returned by the API after creation (use " +
					"`has_admin_password` to check whether one is set). Forces replacement.",
				Required:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(8, 128),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet to attach the instance to. If omitted " +
					"the instance is created in the tenant's default network. Forces replacement.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidPattern, "must be a valid UUID"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a public IP to attach to the instance at creation. " +
					"The Windows API has no live attach/detach for instances, so this is " +
					"sent only in the create body and forces replacement when changed.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidPattern, "must be a valid UUID"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"data_volume_ids": schema.ListAttribute{
				MarkdownDescription: "UUIDs of block volumes to attach to the instance " +
					"(max 5). Forces replacement.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtMost(5),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the instance. Forces replacement.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"license_consent": schema.BoolAttribute{
				MarkdownDescription: "Must be `true`. CETIC Cloud provides no Windows license; " +
					"you are responsible for holding a valid license for each instance. " +
					"Forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "Computed hostname of the Windows instance.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"cores": schema.Int64Attribute{
				MarkdownDescription: "vCPU count derived from the selected plan.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"memory_mb": schema.Int64Attribute{
				MarkdownDescription: "Memory in MB derived from the selected plan.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"disk_gb": schema.Int64Attribute{
				MarkdownDescription: "Root disk size in GB derived from the selected plan.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle state. One of `installing`, " +
					"`provisioning`, `running`, `stopped`, `error`, or `deleting`. After a " +
					"successful apply this will be `running`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ip_address": schema.StringAttribute{
				MarkdownDescription: "Private IPv4 address allocated from the VNet's IPAM.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IPv4 address attached to the instance, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"has_admin_password": schema.BoolAttribute{
				MarkdownDescription: "True when an administrator password was set at creation time.",
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"error_message": schema.StringAttribute{
				MarkdownDescription: "Last error message reported by the provisioner, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the instance was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last server-side update.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

// ModifyPlan rejects an explicit `license_consent = false` at plan time, before
// any API call. CETIC Cloud provides no Windows license, so the caller must
// affirmatively opt in. Skips when the value is null/unknown (destroy plans or
// values not yet resolved).
func (r *windowsInstanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var plan windowsInstanceResourceModel
	if d := req.Plan.Get(ctx, &plan); d.HasError() {
		return
	}
	if plan.LicenseConsent.IsNull() || plan.LicenseConsent.IsUnknown() {
		return
	}
	if !plan.LicenseConsent.ValueBool() {
		resp.Diagnostics.AddError(
			"License consent required",
			"license_consent must be set to true — CETIC Cloud Platform does not "+
				"provide any Windows license; you are responsible for holding a valid "+
				"license for each instance.",
		)
	}
}

func (r *windowsInstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		// Provider not yet configured (e.g. validate-only run). Nothing to do.
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider — please report it.", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *windowsInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan windowsInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Materialise the framework Lists into plain []string. Null/unknown
	// collapse to nil, which the API treats as "absent".
	tags, diags := stringsFromList(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	dataVolumeIDs, diags := stringsFromList(ctx, plan.DataVolumeIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.WindowsInstanceCreateRequest{
		Name:                  plan.Name.ValueString(),
		Region:                plan.Region.ValueString(),
		Plan:                  plan.Plan.ValueString(),
		TemplateKey:           plan.Template.ValueString(),
		AdministratorPassword: plan.AdministratorPassword.ValueString(),
		DataVolumeIDs:         dataVolumeIDs,
		Tags:                  tags,
		LicenseConsent:        plan.LicenseConsent.ValueBool(),
	}
	if !plan.VnetID.IsNull() && !plan.VnetID.IsUnknown() {
		v := plan.VnetID.ValueString()
		createReq.VnetID = &v
	}
	if !plan.PublicIPID.IsNull() && !plan.PublicIPID.IsUnknown() {
		v := plan.PublicIPID.ValueString()
		createReq.PublicIPID = &v
	}

	created, err := r.client.CreateWindowsInstance(ctx, createReq)
	if err != nil {
		// 409 (quota exceeded, VNet inactive, name collision, …) and 429
		// (rate limit / quota) carry a `detail` payload meant to be shown
		// verbatim. Surface them with a dedicated summary.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Windows instance creation conflicts with current state",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create Windows instance",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	// Poll until the instance reaches `running`, unless it is already there.
	if created.Status != client.WindowsStatusRunning {
		if pollErr := pollUntilWindowsReady(ctx, r.client, created.ID); pollErr != nil {
			resp.Diagnostics.AddError(
				"Windows instance failed to reach running state",
				fmt.Sprintf("Windows instance %s did not reach a usable state within %s: %s",
					created.ID, createPollTimeout, pollErr.Error()),
			)
			return
		}
	}

	// Re-fetch the authoritative record — the initial response may not reflect
	// the final IP, plan-derived (cores/memory/disk), or has_admin_password.
	fresh, err := r.client.GetWindowsInstance(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read Windows instance after provisioning",
			fmt.Sprintf("Windows instance %s was created but the follow-up GET failed: %s",
				created.ID, err.Error()),
		)
		return
	}

	diags = applyWindowsInstanceToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The Windows API has no FK on public_ips and never returns the public IP
	// UUID on read, so keep whatever the plan supplied (Optional+Computed).
	if plan.PublicIPID.IsUnknown() {
		plan.PublicIPID = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *windowsInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state windowsInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetWindowsInstance(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: instance was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read Windows instance",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we leave in state as-is — the next
	// refresh will pick up the eventual 404 and remove the resource.
	if got.Status == client.WindowsStatusDeleting {
		state.Status = types.StringValue(got.Status)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	diags := applyWindowsInstanceToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// public_ip_id is never returned by the API — keep the prior state value.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op against the API: every user-settable attribute is
// RequiresReplace, so the framework never invokes Update with a real attribute
// diff. The method exists only because the framework requires it — we copy the
// plan into state and save.
func (r *windowsInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan windowsInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *windowsInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state windowsInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.DeleteWindowsInstance(ctx, id); err != nil {
		// Treat "already gone" as success.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete Windows instance",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return
	}

	// Poll until GetWindowsInstance returns 404. If the timeout elapses, warn
	// but let Terraform remove the resource from state — the backend is still
	// converging asynchronously and blocking the apply would be worse.
	pollErr := client.Poll(ctx, deletePollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetWindowsInstance(ctx, id)
		if err == nil {
			return false, nil
		}
		if client.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if pollErr != nil {
		resp.Diagnostics.AddWarning(
			"Windows instance deletion did not complete within the timeout",
			fmt.Sprintf("Windows instance %s was scheduled for deletion but did not "+
				"disappear within %s: %s. Terraform will remove the resource from state; "+
				"the backend should finish the teardown asynchronously.",
				id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing instance with `terraform import
// ccp_windows_instance.example <uuid>`. Read fills the rest.
func (r *windowsInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// pollUntilWindowsReady polls GetWindowsInstance every createPollInterval up to
// createPollTimeout, returning nil once the instance reaches `running`, or a
// non-nil error on `error` status, an API error, or a real polling timeout
// while still installing/provisioning.
func pollUntilWindowsReady(ctx context.Context, c *client.Client, id string) error {
	return client.Poll(ctx, createPollInterval, createPollTimeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetWindowsInstance(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case client.WindowsStatusError:
			errMsg := ""
			if cur.ErrorMessage != nil {
				errMsg = *cur.ErrorMessage
			}
			return false, fmt.Errorf("windows instance %s entered error state during provisioning: %s", cur.ID, errMsg)
		case client.WindowsStatusRunning:
			return true, nil
		default:
			// installing / provisioning — keep polling.
			return false, nil
		}
	})
}

// applyWindowsInstanceToModel populates the framework model from the API
// representation. Always called after a successful Create/Read so state
// reflects the authoritative server view. Tags are normalised so a `nil` API
// response and an empty list both produce an empty list in state.
func applyWindowsInstanceToModel(ctx context.Context, src *client.WindowsInstance, dst *windowsInstanceResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.Name = types.StringValue(src.Name)
	dst.Region = types.StringValue(src.Region)
	dst.Plan = types.StringValue(src.Plan)
	dst.Template = types.StringValue(src.Template)
	dst.Hostname = types.StringValue(src.Hostname)
	dst.Cores = types.Int64Value(int64(src.Cores))
	dst.MemoryMB = types.Int64Value(int64(src.MemoryMB))
	dst.DiskGB = types.Int64Value(int64(src.DiskGB))
	dst.Status = types.StringValue(src.Status)
	dst.HasAdminPassword = types.BoolValue(src.HasAdminPassword)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))
	dst.UpdatedAt = types.StringValue(src.UpdatedAt.Format(time.RFC3339))

	dst.IPAddress = stringPtrToValue(src.PrivateIP)
	dst.PublicIPAddress = stringPtrToValue(src.PublicIPAddress)
	dst.ErrorMessage = stringPtrToValue(src.ErrorMessage)

	// vnet_id is Optional (no Computed) — only mirror it back if the planned
	// attribute was unknown (i.e. on first Create or import). For subsequent
	// reads, leave whatever the configuration says intact.
	if dst.VnetID.IsUnknown() {
		dst.VnetID = stringPtrToValue(src.VnetID)
	}
	if dst.AdministratorPassword.IsUnknown() {
		// Never readable from the API — has_admin_password is authoritative.
		dst.AdministratorPassword = types.StringNull()
	}

	tagValues := make([]string, 0, len(src.Tags))
	tagValues = append(tagValues, src.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	dst.Tags = tagsList

	// data_volume_ids: preserve the planned/imported value. If unknown (e.g.
	// on import), fall back to the API list.
	if dst.DataVolumeIDs.IsUnknown() {
		dvValues := make([]string, 0, len(src.DataVolumeIDs))
		dvValues = append(dvValues, src.DataVolumeIDs...)
		dvList, dvDiags := types.ListValueFrom(ctx, types.StringType, dvValues)
		diags.Append(dvDiags...)
		if diags.HasError() {
			return diags
		}
		dst.DataVolumeIDs = dvList
	}

	return diags
}

// stringPtrToValue collapses a *string into a framework String value: nil →
// Null, otherwise the underlying string.
func stringPtrToValue(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// stringsFromList converts the framework List representation into a Go slice.
// Null and unknown both collapse to nil so callers can hand the result straight
// to the API client.
func stringsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	out := make([]string, 0, len(list.Elements()))
	diags := list.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		return nil, diags
	}
	return out, diags
}
