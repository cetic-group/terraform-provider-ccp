// Package vminstance implements the ccp_vm_instance Terraform resource.
//
// A VM instance in CETIC Cloud is a Proxmox QEMU virtual machine cloned from a
// pre-existing template (Ubuntu/Debian cloud images) and configured at first
// boot via cloud-init (sshkeys, user_data, root password). The CETIC Cloud API
// supports an in-place PATCH that mutates only `name` and `tags`; every other
// user-settable attribute (`region`, `plan`, `template`, `vnet_id`,
// `ssh_key_ids`, `user_data`, `public_ip_id`, `root_password`) forces
// replacement on change.
//
// Provisioning is asynchronous: POST /v1/vm-instances returns 201 with
// `status=provisioning` and a Celery worker performs the `qm clone`, disk
// resize, cloud-init drive build, and first boot. We poll GetVMInstance for
// up to 10 minutes (VMs are slower than containers because of cloud-init +
// apt + qemu-guest-agent) until the VM reaches `running` with a resolved IP
// address. If the VM reports `error`, we fail with the API-side
// error_message. If it reaches `running` without an IP within the timeout,
// we surface a warning rather than an error and let a subsequent refresh
// pick up the address.
//
// Deletion is also asynchronous: the VM enters `deleting` and disappears
// from the API once the QEMU teardown + IPAM release completes. We poll for
// 404 up to 5 minutes and surface a warning rather than an error if the
// timeout elapses, since the resource will still be removed from Terraform
// state and the API is converging on its own.
package vminstance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/snatvalidator"
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
	_ resource.Resource                = (*vmInstanceResource)(nil)
	_ resource.ResourceWithConfigure   = (*vmInstanceResource)(nil)
	_ resource.ResourceWithImportState = (*vmInstanceResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*vmInstanceResource)(nil)
)

// New returns a freshly-constructed ccp_vm_instance resource.
// Wired in by provider.go via vminstance.New.
func New() resource.Resource {
	return &vmInstanceResource{}
}

// vmInstanceResource is the framework Resource implementation. The client is
// stashed in Configure and reused by Create/Read/Update/Delete.
type vmInstanceResource struct {
	client *client.Client
}

// vmInstanceResourceModel mirrors the schema below 1-to-1. Tag names must
// match the schema attribute keys exactly.
type vmInstanceResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Region                types.String `tfsdk:"region"`
	Plan                  types.String `tfsdk:"plan"`
	Template              types.String `tfsdk:"template"`
	VnetID                types.String `tfsdk:"vnet_id"`
	SSHKeyIDs             types.List   `tfsdk:"ssh_key_ids"`
	UserData              types.String `tfsdk:"user_data"`
	PublicIPID            types.String `tfsdk:"public_ip_id"`
	RootPassword          types.String `tfsdk:"root_password"`
	BastionAccess         types.Bool   `tfsdk:"bastion_access"`
	WindowsLicenseConsent types.Bool   `tfsdk:"windows_license_consent"`
	Tags                  types.List   `tfsdk:"tags"`
	OsFamily              types.String `tfsdk:"os_family"`
	Cores                 types.Int64  `tfsdk:"cores"`
	MemoryMB              types.Int64  `tfsdk:"memory_mb"`
	DiskGB                types.Int64  `tfsdk:"disk_gb"`
	Status                types.String `tfsdk:"status"`
	IPAddress             types.String `tfsdk:"ip_address"`
	PublicIPAddress       types.String `tfsdk:"public_ip_address"`
	ScaleSetID            types.String `tfsdk:"scale_set_id"`
	ErrorMessage          types.String `tfsdk:"error_message"`
	HasRootPassword       types.Bool   `tfsdk:"has_root_password"`
	CreatedAt             types.String `tfsdk:"created_at"`
	UpdatedAt             types.String `tfsdk:"updated_at"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscores
// and hyphens, 1..100 chars.
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,100}$`)

// uuidPattern is a permissive RFC 4122 matcher for CETIC Cloud resource IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Polling parameters.
//   - Create waits up to 10 minutes for the VM to leave `provisioning`. VMs
//     are slower than containers because cloud-init runs apt + installs
//     qemu-guest-agent before the IP is published.
//   - Delete waits up to 5 minutes for the VM to disappear (404).
const (
	createPollInterval = 5 * time.Second
	createPollTimeout  = 10 * time.Minute
	deletePollInterval = 5 * time.Second
	deletePollTimeout  = 5 * time.Minute
)

// defaultTemplate is applied when the user does not specify one. Matches the
// CETIC Cloud catalogue default.
const defaultTemplate = "ubuntu-24.04"

func (r *vmInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vm_instance"
}

func (r *vmInstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VM instance (QEMU). Only `name` and `tags` " +
			"are mutable in place via the API's PATCH endpoint; every other user-settable " +
			"attribute forces replacement. Creation is asynchronous: the provider polls until " +
			"the VM reaches the `running` state with a resolved IP address (or up to 10 minutes).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the VM instance.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "VM name (1–100 chars; alphanumerics, `_`, and `-`). " +
					"Mutable in place via PATCH.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must be 1–100 chars containing only letters, digits, underscores, or hyphens",
					),
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
			"template": schema.StringAttribute{
				MarkdownDescription: "Template key (e.g. `ubuntu-24.04`, `debian-12`). Must " +
					"match an active template registered in the CETIC Cloud catalogue. " +
					"Defaults to `ubuntu-24.04`. Forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet to attach the VM to. If omitted the " +
					"VM is created in the tenant's default network. Forces replacement.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidPattern, "must be a valid UUID"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"ssh_key_ids": schema.ListAttribute{
				MarkdownDescription: "UUIDs of SSH keys to inject via cloud-init. Changes " +
					"force replacement (cloud-init only runs on first boot).",
				Optional:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "Cloud-init user data (max 65536 bytes). Applied at " +
					"first boot only; changes force replacement.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(65536),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"bastion_access": schema.BoolAttribute{
				MarkdownDescription: "Allow SSH access to the VM through the tenant Bastion " +
					"(opt-in, default false). Write-only — the API does not return this field on " +
					"read, so changes force replacement.",
				Optional: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"windows_license_consent": schema.BoolAttribute{
				MarkdownDescription: "Acknowledge that CETIC Cloud provides no Windows license: " +
					"you are responsible for acquiring and holding a valid Windows license for " +
					"each instance. **Required (`true`) when `template` is a Windows system image " +
					"(`win-*`) or a custom template captured from a Windows VM** — the API rejects " +
					"the create with HTTP 422 otherwise. Windows instances also require a strong " +
					"administrator password (≥ 12 characters, ≥ 3 of: lowercase, uppercase, digit, " +
					"symbol) and a plan of `medium` or larger. Ignored for Linux templates. " +
					"Write-only — not returned on read, so changes force replacement.",
				Optional: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a public IP to attach to the VM. Mutable: " +
					"changing this attribute attaches/detaches via the CETIC Cloud API " +
					"without recreating the VM. Set at creation to pin the IP from the " +
					"start; update later to swap; clear to detach.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidPattern, "must be a valid UUID"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"root_password": schema.StringAttribute{
				MarkdownDescription: "Root password set at first boot. **Required** (CCP " +
					"API ≥ v1.4.0 enforces a non-empty password, 8-128 chars). Sensitive: " +
					"never returned by the API after creation (use `has_root_password` to " +
					"check whether one is set). Forces replacement.",
				Required:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(8, 128),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the VM. Mutable in place " +
					"via PATCH.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"os_family": schema.StringAttribute{
				MarkdownDescription: "Operating system family derived from the instance " +
					"template: `linux` or `windows`. Windows instances are accessed over RDP " +
					"(no SSH); their administrator account is `Administrator`.",
				Computed: true,
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
				MarkdownDescription: "Current lifecycle state. One of `provisioning`, " +
					"`running`, `stopped`, `error`, or `deleting`. After a successful apply " +
					"this will be `running`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ip_address": schema.StringAttribute{
				MarkdownDescription: "Private IPv4 address allocated from the VNet's IPAM. " +
					"May be empty briefly after provisioning until cloud-init completes and " +
					"qemu-guest-agent reports the address.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IPv4 address attached to the VM, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"scale_set_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent VM scale set, if this VM was " +
					"created as part of one. Stand-alone VMs have a null value.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"error_message": schema.StringAttribute{
				MarkdownDescription: "Last error message reported by the provisioner, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"has_root_password": schema.BoolAttribute{
				MarkdownDescription: "True when a root password was set at creation time.",
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the VM was created.",
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

// ModifyPlan validates VNet egress requirements before apply. When the
// user supplies a `user_data` script, the target VNet MUST have
// `snat=true` — otherwise the script's package downloads (apt, curl, etc.)
// will silently fail at first boot and leave the VM in a broken state.
// Skips during destroy plans and when `vnet_id` / `user_data` are unknown.
func (r *vmInstanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	if r.client == nil {
		return // provider not configured yet
	}
	var plan vmInstanceResourceModel
	if d := req.Plan.Get(ctx, &plan); d.HasError() {
		return
	}
	if plan.UserData.IsNull() || plan.UserData.IsUnknown() || plan.UserData.ValueString() == "" {
		return
	}
	if plan.VnetID.IsNull() || plan.VnetID.IsUnknown() || plan.VnetID.ValueString() == "" {
		return
	}
	snatvalidator.CheckVnetSnat(
		ctx, r.client, plan.VnetID.ValueString(),
		"Cloud-init `user_data` scripts that install packages, fetch container "+
			"images, or reach any external endpoint will fail without outbound "+
			"NAT.",
		&resp.Diagnostics,
	)
}

func (r *vmInstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *vmInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmInstanceResourceModel
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
	sshKeyIDs, diags := stringsFromList(ctx, plan.SSHKeyIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve template: explicit value wins, otherwise apply the catalogue
	// default. The schema marks template as Optional+Computed so the planned
	// value is unknown when the user didn't set it.
	tmpl := defaultTemplate
	if !plan.Template.IsNull() && !plan.Template.IsUnknown() {
		tmpl = plan.Template.ValueString()
	}

	createReq := client.VMInstanceCreateRequest{
		Name:      plan.Name.ValueString(),
		Region:    plan.Region.ValueString(),
		Plan:      plan.Plan.ValueString(),
		Template:  tmpl,
		SSHKeyIDs: sshKeyIDs,
		Tags:      tags,
	}
	if !plan.VnetID.IsNull() && !plan.VnetID.IsUnknown() {
		v := plan.VnetID.ValueString()
		createReq.VnetID = &v
	}
	if !plan.UserData.IsNull() && !plan.UserData.IsUnknown() {
		v := plan.UserData.ValueString()
		createReq.UserData = &v
	}
	if !plan.PublicIPID.IsNull() && !plan.PublicIPID.IsUnknown() {
		v := plan.PublicIPID.ValueString()
		createReq.PublicIPID = &v
	}
	if !plan.RootPassword.IsNull() && !plan.RootPassword.IsUnknown() {
		v := plan.RootPassword.ValueString()
		createReq.RootPassword = &v
	}
	if !plan.BastionAccess.IsNull() && !plan.BastionAccess.IsUnknown() {
		createReq.BastionAccess = plan.BastionAccess.ValueBool()
	}
	if !plan.WindowsLicenseConsent.IsNull() && !plan.WindowsLicenseConsent.IsUnknown() {
		createReq.WindowsLicenseConsent = plan.WindowsLicenseConsent.ValueBool()
	}

	created, err := r.client.CreateVMInstance(ctx, createReq)
	if err != nil {
		// 409 (quota exceeded, VNet inactive, name collision, …) and 429
		// (rate limit / quota) carry a `detail` payload that is meant to be
		// shown verbatim. Surface them with a dedicated summary so users can
		// distinguish them from generic API failures.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VM creation conflicts with current state",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create VM",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	// Poll until the VM leaves `provisioning`. Success criterion:
	// status == running AND ip_address resolved. We also accept `running`
	// without an IP after the timeout, but with a warning so users notice.
	timedOutWithoutIP := false

	switch created.Status {
	case client.VMStatusError:
		errMsg := ""
		if created.ErrorMessage != nil {
			errMsg = *created.ErrorMessage
		}
		resp.Diagnostics.AddError(
			"VM entered error state during provisioning",
			fmt.Sprintf("VM %s reported status `error` immediately after creation. "+
				"API error_message: %q.", created.ID, errMsg),
		)
		return
	case client.VMStatusRunning:
		if created.IPAddress == nil || *created.IPAddress == "" {
			// Reached running but no IP yet — fall through to polling so we
			// give cloud-init a chance to publish the address.
			pollErr := pollUntilRunningWithIP(ctx, r.client, created.ID)
			if pollErr != nil {
				if errors.Is(pollErr, errPollTimeoutNoIP) {
					timedOutWithoutIP = true
				} else {
					resp.Diagnostics.AddError(
						"VM failed to reach running state with IP",
						fmt.Sprintf("VM %s did not reach a usable state within %s: %s",
							created.ID, createPollTimeout, pollErr.Error()),
					)
					return
				}
			}
		}
	default:
		pollErr := pollUntilRunningWithIP(ctx, r.client, created.ID)
		if pollErr != nil {
			if errors.Is(pollErr, errPollTimeoutNoIP) {
				timedOutWithoutIP = true
			} else {
				resp.Diagnostics.AddError(
					"VM failed to reach running state",
					fmt.Sprintf("VM %s did not reach a usable state within %s: %s",
						created.ID, createPollTimeout, pollErr.Error()),
				)
				return
			}
		}
	}

	// Re-fetch the authoritative record — the initial response may not
	// reflect the final IP, error_message, plan-derived (cores/memory/disk),
	// or has_root_password fields.
	fresh, err := r.client.GetVMInstance(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read VM after provisioning",
			fmt.Sprintf("VM %s was created but the follow-up GET failed: %s",
				created.ID, err.Error()),
		)
		return
	}

	if timedOutWithoutIP {
		resp.Diagnostics.AddWarning(
			"VM is running but IP address has not yet been resolved",
			fmt.Sprintf("VM %s reached `running` within %s but the IP address is "+
				"not yet visible to the API. It will appear on the next refresh "+
				"(`terraform refresh` or the next `terraform plan`).",
				created.ID, createPollTimeout),
		)
	}

	diags = applyVMInstanceToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	refreshPublicIPID(ctx, r.client, fresh.ID, &plan.PublicIPID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetVMInstance(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: VM was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VM",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we leave in state as-is — the next
	// refresh will pick up the eventual 404 and remove the resource.
	if got.Status == client.VMStatusDeleting {
		// Still update the status field so plans show the in-flight deletion.
		state.Status = types.StringValue(got.Status)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	diags := applyVMInstanceToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	refreshPublicIPID(ctx, r.client, got.ID, &state.PublicIPID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// refreshPublicIPID resolves the UUID of the public IP currently attached
// to the VM via `client.FindPublicIPByResource`. `GET /v1/vm-instances/{id}`
// only returns the resolved IPv4 address, not the UUID, so we list+filter
// to keep `public_ip_id` accurate in state for Read/Create/Update paths.
// Quiet failures (network blip, transient API error) leave the prior value
// untouched — the next plan/refresh will retry.
func refreshPublicIPID(ctx context.Context, c *client.Client, vmID string, dst *types.String) {
	if c == nil || vmID == "" {
		return
	}
	ip, err := c.FindPublicIPByResource(ctx, "vm_instance", vmID)
	if err != nil {
		return
	}
	if ip == nil {
		*dst = types.StringNull()
		return
	}
	*dst = types.StringValue(ip.ID)
}

// Update handles in-place mutation of `name`, `tags`, and `public_ip_id`.
// Name/tags go through PATCH /v1/vm-instances/{id}; public_ip_id swaps are
// performed via the dedicated /v1/public-ips/{id}/attach + /detach endpoints,
// allowing the VM itself to keep running across the change.
func (r *vmInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vmInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// ── Public IP attach/detach via dedicated endpoints (no VM recreate) ──
	planIPID := plan.PublicIPID.ValueString()
	stateIPID := state.PublicIPID.ValueString()
	if planIPID != stateIPID {
		// Detach old IP first to avoid 409 from the API (an IP can only be
		// attached to one resource at a time).
		if stateIPID != "" {
			if _, err := r.client.DetachPublicIP(ctx, stateIPID); err != nil && !client.IsNotFound(err) {
				resp.Diagnostics.AddError(
					"Failed to detach public IP from VM",
					fmt.Sprintf("CETIC Cloud API error detaching IP %s from VM %s: %s",
						stateIPID, id, err.Error()),
				)
				return
			}
		}
		if planIPID != "" {
			if _, err := r.client.AttachPublicIP(ctx, planIPID, client.PublicIPAttachRequest{
				ResourceType: "vm_instance",
				ResourceID:   id,
			}); err != nil {
				resp.Diagnostics.AddError(
					"Failed to attach public IP to VM",
					fmt.Sprintf("CETIC Cloud API error attaching IP %s to VM %s: %s",
						planIPID, id, err.Error()),
				)
				return
			}
		}
	}

	planTags, diags := stringsFromList(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	stateTags, diags := stringsFromList(ctx, state.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	nameChanged := plan.Name.ValueString() != state.Name.ValueString()
	tagsChanged := !stringSlicesEqual(planTags, stateTags)

	if nameChanged || tagsChanged {
		var updateReq client.VMInstanceUpdateRequest
		if nameChanged {
			n := plan.Name.ValueString()
			updateReq.Name = &n
		}
		if tagsChanged {
			// PATCH expects the full tag list; nil is "absent" (no-op), so
			// when clearing tags we pass an empty (non-nil) slice. JSON tag
			// `omitempty` means an empty slice would be omitted on the wire,
			// but the API treats absence and empty-list identically for tags.
			if planTags == nil {
				updateReq.Tags = []string{}
			} else {
				updateReq.Tags = planTags
			}
		}

		if _, err := r.client.UpdateVMInstance(ctx, id, updateReq); err != nil {
			if client.IsConflict(err) {
				resp.Diagnostics.AddError(
					"VM update conflicts with current state",
					fmt.Sprintf("CETIC Cloud rejected the PATCH call for VM %s: %s", id, err.Error()),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Failed to update VM",
				fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
			)
			return
		}
	}

	// Re-fetch so state reflects the authoritative server view (timestamps,
	// normalised tag order, etc.).
	fresh, err := r.client.GetVMInstance(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read VM after update",
			fmt.Sprintf("VM %s was updated but the follow-up GET failed: %s", id, err.Error()),
		)
		return
	}

	diags = applyVMInstanceToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	refreshPublicIPID(ctx, r.client, fresh.ID, &plan.PublicIPID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vmInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.DeleteVMInstance(ctx, id); err != nil {
		// Treat "already gone" as success — no point erroring on destroy when
		// the desired end state is already reached.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete VM",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return
	}

	// Poll until GetVMInstance returns 404. If the timeout elapses, warn but
	// let Terraform remove the resource from state — CETIC Cloud is still
	// converging asynchronously and blocking the apply would be worse.
	pollErr := client.Poll(ctx, deletePollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetVMInstance(ctx, id)
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
			"VM deletion did not complete within the timeout",
			fmt.Sprintf("VM %s was scheduled for deletion but did not disappear "+
				"within %s: %s. Terraform will remove the resource from state; the Cloud "+
				"Lake backend should finish the teardown asynchronously.",
				id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing VM with `terraform import
// ccp_vm_instance.example <uuid>`. Read fills the rest.
func (r *vmInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// errPollTimeoutNoIP is a sentinel returned by pollUntilRunningWithIP when
// the VM reached `running` but the IP address never resolved within the
// timeout. Distinct from a real polling error so the caller can downgrade
// it to a warning.
var errPollTimeoutNoIP = errors.New("vm reached running state but IP address was not resolved within timeout")

// pollUntilRunningWithIP polls GetVMInstance every createPollInterval up to
// createPollTimeout, returning:
//   - nil if the VM reaches `running` with a non-empty ip_address;
//   - errPollTimeoutNoIP if the VM reaches `running` but the IP is still
//     unresolved when the timeout elapses;
//   - a non-nil error in every other failure case (status `error`, API error,
//     real polling timeout while still in `provisioning`, …).
func pollUntilRunningWithIP(ctx context.Context, c *client.Client, id string) error {
	deadline := time.Now().Add(createPollTimeout)
	reachedRunning := false
	var lastErrMsg string

	pollErr := client.Poll(ctx, createPollInterval, createPollTimeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetVMInstance(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case client.VMStatusError:
			if cur.ErrorMessage != nil {
				lastErrMsg = *cur.ErrorMessage
			}
			return false, fmt.Errorf("vm %s entered error state during provisioning: %s", cur.ID, lastErrMsg)
		case client.VMStatusRunning:
			reachedRunning = true
			if cur.IPAddress != nil && *cur.IPAddress != "" {
				return true, nil
			}
			// Running but no IP yet — keep polling until the deadline.
			if time.Now().After(deadline) {
				return true, nil
			}
			return false, nil
		default:
			return false, nil
		}
	})
	if pollErr != nil {
		return pollErr
	}
	if reachedRunning {
		// Final check: if we exited via the deadline branch with no IP,
		// distinguish that case so the caller can warn rather than error.
		cur, err := c.GetVMInstance(ctx, id)
		if err == nil && (cur.IPAddress == nil || *cur.IPAddress == "") && cur.Status == client.VMStatusRunning {
			return errPollTimeoutNoIP
		}
	}
	return nil
}

// applyVMInstanceToModel populates the framework model from the API
// representation. Always called after a successful Create/Read/Update so
// state reflects the authoritative server view. Tags are normalised so a
// `nil` API response and an empty list both produce an empty list in state
// (avoids spurious diffs against an Optional+Computed list attribute).
func applyVMInstanceToModel(ctx context.Context, src *client.VMInstance, dst *vmInstanceResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.Name = types.StringValue(src.Name)
	dst.Region = types.StringValue(src.Region)
	dst.Plan = types.StringValue(src.Plan)
	dst.Template = types.StringValue(src.Template)
	dst.Cores = types.Int64Value(int64(src.Cores))
	dst.MemoryMB = types.Int64Value(int64(src.MemoryMB))
	dst.DiskGB = types.Int64Value(int64(src.DiskGB))
	dst.Status = types.StringValue(src.Status)
	dst.HasRootPassword = types.BoolValue(src.HasRootPassword)
	osFamily := src.OSFamily
	if osFamily == "" {
		osFamily = "linux"
	}
	dst.OsFamily = types.StringValue(osFamily)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))
	dst.UpdatedAt = types.StringValue(src.UpdatedAt.Format(time.RFC3339))

	dst.IPAddress = stringPtrToValue(src.IPAddress)
	dst.PublicIPAddress = stringPtrToValue(src.PublicIPAddress)
	dst.ScaleSetID = stringPtrToValue(src.ScaleSetID)
	dst.ErrorMessage = stringPtrToValue(src.ErrorMessage)

	// vnet_id is Optional (no Computed) — only mirror it back if the planned
	// attribute was unknown (i.e. on first Create or import). For subsequent
	// reads, leave whatever the configuration says intact to avoid spurious
	// drift between null plan and non-null state.
	if dst.VnetID.IsUnknown() {
		dst.VnetID = stringPtrToValue(src.VnetID)
	}
	// Same idea for user_data: if the user did not configure it, the API
	// won't echo it back (and even if it did we'd want to keep state aligned
	// with the configured plan).
	if dst.UserData.IsUnknown() {
		dst.UserData = stringPtrToValue(src.UserData)
	}
	if dst.PublicIPID.IsUnknown() {
		// public_ip_id isn't returned directly by GET /v1/vm-instances/{id};
		// the API only exposes the resolved address. Leave whatever the plan
		// said (or null) intact.
		dst.PublicIPID = types.StringNull()
	}
	if dst.RootPassword.IsUnknown() {
		// Never readable from the API — the boolean has_root_password is
		// authoritative. Mirror null so subsequent plans stay clean.
		dst.RootPassword = types.StringNull()
	}

	tagValues := make([]string, 0, len(src.Tags))
	tagValues = append(tagValues, src.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	dst.Tags = tagsList

	// ssh_key_ids: the API stores these but does not return them on GET.
	// Preserve whatever was planned/imported. If unknown (e.g. on import),
	// fall back to an empty list so we don't surface a permanent unknown.
	if dst.SSHKeyIDs.IsUnknown() {
		emptyList, listDiags := types.ListValueFrom(ctx, types.StringType, []string{})
		diags.Append(listDiags...)
		if diags.HasError() {
			return diags
		}
		dst.SSHKeyIDs = emptyList
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
// Null and unknown both collapse to nil so callers can hand the result
// straight to the API client.
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

// stringSlicesEqual compares two []string by length and element-wise (order
// significant). Used to decide whether the tag list changed across a plan/
// state delta — the API treats tag order as significant in the PATCH body.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
