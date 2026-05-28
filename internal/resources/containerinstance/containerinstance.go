// Package containerinstance implements the ccp_container_instance
// Terraform resource.
//
// A container instance in CETIC Cloud is a Proxmox LXC container provisioned
// from a template (Ubuntu/Debian/...). The API exposes no PATCH endpoint, so
// every user-settable attribute (`name`, `region`, `plan`, `template`,
// `vnet_id`, `ssh_key_ids`, `user_data`, `public_ip_id`, `root_password`,
// `tags`) forces replacement on change.
//
// Provisioning is asynchronous: POST /v1/containers returns 201 with
// `status=provisioning` and a Celery worker performs the actual `pct create`
// + cloud-init wait + IPAM allocation. We poll GetContainer for up to 5
// minutes until the container reaches `running` with a resolved IP address.
// If the container reports `error`, we fail with the API-side error_message.
// If it reaches `running` but the IP has not yet been resolved within the
// timeout, we surface a warning rather than an error and let a subsequent
// refresh pick up the IP.
//
// Deletion is also asynchronous: the container enters `deleting` and
// disappears from the API once the LXC teardown + IPAM release completes.
// We poll for 404 up to 180 s and surface a warning rather than an error if
// the timeout elapses, since the resource will still be removed from
// Terraform state and the API is converging on its own.
package containerinstance

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/snatvalidator"
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
	_ resource.Resource                = (*containerInstanceResource)(nil)
	_ resource.ResourceWithConfigure   = (*containerInstanceResource)(nil)
	_ resource.ResourceWithImportState = (*containerInstanceResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*containerInstanceResource)(nil)
)

// New returns a freshly-constructed ccp_container_instance resource.
// Wired in by provider.go via containerinstance.New.
func New() resource.Resource {
	return &containerInstanceResource{}
}

// containerInstanceResource is the framework Resource implementation. The
// client is stashed in Configure and reused by Create/Read/Update/Delete.
type containerInstanceResource struct {
	client *client.Client
}

// containerInstanceResourceModel mirrors the schema below 1-to-1. Tag names
// must match the schema attribute keys exactly.
type containerInstanceResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	Plan            types.String `tfsdk:"plan"`
	Template        types.String `tfsdk:"template"`
	VnetID          types.String `tfsdk:"vnet_id"`
	SSHKeyIDs       types.List   `tfsdk:"ssh_key_ids"`
	UserData        types.String `tfsdk:"user_data"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	RootPassword    types.String `tfsdk:"root_password"`
	Tags            types.List   `tfsdk:"tags"`
	Cores           types.Int64  `tfsdk:"cores"`
	MemoryMB        types.Int64  `tfsdk:"memory_mb"`
	DiskGB          types.Int64  `tfsdk:"disk_gb"`
	Status          types.String `tfsdk:"status"`
	IPAddress       types.String `tfsdk:"ip_address"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
	ScaleSetID      types.String `tfsdk:"scale_set_id"`
	ErrorMessage    types.String `tfsdk:"error_message"`
	HasRootPassword types.Bool   `tfsdk:"has_root_password"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscores
// and hyphens, 1..100 chars.
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,100}$`)

// uuidPattern is a permissive RFC 4122 matcher for CETIC Cloud resource IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Polling parameters.
//   - Create waits up to 5 minutes for the container to leave `provisioning`.
//   - Delete waits up to 3 minutes for the container to disappear (404).
const (
	createPollInterval = 5 * time.Second
	createPollTimeout  = 5 * time.Minute
	deletePollInterval = 5 * time.Second
	deletePollTimeout  = 3 * time.Minute
)

func (r *containerInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_container_instance"
}

func (r *containerInstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud container instance (LXC). The API has no " +
			"in-place update endpoint, so every user-settable attribute forces replacement. " +
			"Creation is asynchronous: the provider polls until the container reaches the " +
			"`running` state with a resolved IP address (or up to 5 minutes).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the container instance.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Container name (1–100 chars; alphanumerics, `_`, and `-`).",
				Required:            true,
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
					"`PAR` (Paris, France), or `ABJ` (Abidjan, Côte d'Ivoire).",
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
					"`large`, or `xlarge`. Each plan maps to a fixed (cores, memory, disk) tuple.",
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
					"match an active template registered in the CETIC Cloud catalogue.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet to attach the container to. If omitted " +
					"the container is created in the tenant's default network. Cannot be moved " +
					"after creation — changing this attribute forces replacement.",
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
					"force replacement (the API has no endpoint to rotate keys in place).",
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
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a public IP to attach to the container. " +
					"Mutable: changing this attribute attaches/detaches via the CETIC " +
					"Cloud API without recreating the container. Set at creation to pin " +
					"the IP from the start; update later to swap; clear to detach.",
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
					"check whether one is set). Changes force replacement.",
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
				MarkdownDescription: "Free-form labels attached to the container. The API has " +
					"no endpoint to mutate tags after creation, so changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
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
					"May be empty briefly after provisioning until cloud-init completes.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IPv4 address attached to the container, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"scale_set_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent container scale set, if this " +
					"container was created as part of one. Stand-alone containers have a null value.",
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
				MarkdownDescription: "RFC 3339 timestamp at which the container was created.",
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

// ModifyPlan validates that the target VNet has outbound internet egress
// when the user supplies a `user_data` cloud-init script. Without
// `snat=true`, package installs at first boot fail silently.
func (r *containerInstanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	if r.client == nil {
		return
	}
	var plan containerInstanceResourceModel
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
		"Cloud-init `user_data` scripts that install packages or fetch any "+
			"external endpoint will fail without outbound NAT.",
		&resp.Diagnostics,
	)
}

func (r *containerInstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *containerInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerInstanceResourceModel
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

	createReq := client.ContainerCreateRequest{
		Name:      plan.Name.ValueString(),
		Region:    plan.Region.ValueString(),
		Plan:      plan.Plan.ValueString(),
		Template:  plan.Template.ValueString(),
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

	created, err := r.client.CreateContainer(ctx, createReq)
	if err != nil {
		// 409 (quota exceeded, VNet inactive, name collision, …) and 429
		// (rate limit / quota) carry a `detail` payload that is meant to be
		// shown verbatim. Surface them with a dedicated summary so users can
		// distinguish them from generic API failures.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Container creation conflicts with current state",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create container",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	// Poll until the container leaves `provisioning`. Success criterion:
	// status == running AND ip_address resolved. We also accept `running`
	// without an IP after the timeout, but with a warning so users notice.
	final := created
	timedOutWithoutIP := false

	switch created.Status {
	case client.ContainerStatusError:
		errMsg := ""
		if created.ErrorMessage != nil {
			errMsg = *created.ErrorMessage
		}
		resp.Diagnostics.AddError(
			"Container entered error state during provisioning",
			fmt.Sprintf("Container %s reported status `error` immediately after creation. "+
				"API error_message: %q.", created.ID, errMsg),
		)
		return
	case client.ContainerStatusRunning:
		if created.IPAddress == nil || *created.IPAddress == "" {
			// Reached running but no IP yet — fall through to polling so we
			// give cloud-init a chance to publish the address.
			pollErr := pollUntilRunningWithIP(ctx, r.client, created.ID)
			if pollErr != nil {
				if pollErr == errPollTimeoutNoIP {
					timedOutWithoutIP = true
				} else {
					resp.Diagnostics.AddError(
						"Container failed to reach running state with IP",
						fmt.Sprintf("Container %s did not reach a usable state within %s: %s",
							created.ID, createPollTimeout, pollErr.Error()),
					)
					return
				}
			}
		}
	default:
		pollErr := pollUntilRunningWithIP(ctx, r.client, created.ID)
		if pollErr != nil {
			if pollErr == errPollTimeoutNoIP {
				timedOutWithoutIP = true
			} else {
				resp.Diagnostics.AddError(
					"Container failed to reach running state",
					fmt.Sprintf("Container %s did not reach a usable state within %s: %s",
						created.ID, createPollTimeout, pollErr.Error()),
				)
				return
			}
		}
	}

	// Re-fetch the authoritative record — the initial response may not
	// reflect the final IP, error_message, plan-derived (cores/memory/disk),
	// or has_root_password fields.
	fresh, err := r.client.GetContainer(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read container after provisioning",
			fmt.Sprintf("Container %s was created but the follow-up GET failed: %s",
				created.ID, err.Error()),
		)
		return
	}
	final = fresh

	if timedOutWithoutIP {
		resp.Diagnostics.AddWarning(
			"Container is running but IP address has not yet been resolved",
			fmt.Sprintf("Container %s reached `running` within %s but the IP address is "+
				"not yet visible to the API. It will be available on the next refresh "+
				"(`terraform refresh` or the next `terraform plan`).",
				created.ID, createPollTimeout),
		)
	}

	diags = applyContainerToModel(ctx, final, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	refreshPublicIPID(ctx, r.client, final.ID, &plan.PublicIPID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetContainer(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: container was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read container",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we leave in state as-is — the next
	// refresh will pick up the eventual 404 and remove the resource.
	if got.Status == client.ContainerStatusDeleting {
		// Still update the status field so plans show the in-flight deletion.
		state.Status = types.StringValue(got.Status)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	diags := applyContainerToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	refreshPublicIPID(ctx, r.client, got.ID, &state.PublicIPID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update handles in-place swapping of `public_ip_id`. All other settable
// attributes carry RequiresReplace, so the framework only routes here when
// the public IP attachment changes.
func (r *containerInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state containerInstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	planIPID := plan.PublicIPID.ValueString()
	stateIPID := state.PublicIPID.ValueString()
	if planIPID != stateIPID {
		if stateIPID != "" {
			if _, err := r.client.DetachPublicIP(ctx, stateIPID); err != nil && !client.IsNotFound(err) {
				resp.Diagnostics.AddError(
					"Failed to detach public IP from container",
					fmt.Sprintf("CETIC Cloud API error detaching IP %s from container %s: %s",
						stateIPID, id, err.Error()),
				)
				return
			}
		}
		if planIPID != "" {
			if _, err := r.client.AttachPublicIP(ctx, planIPID, client.PublicIPAttachRequest{
				ResourceType: "container",
				ResourceID:   id,
			}); err != nil {
				resp.Diagnostics.AddError(
					"Failed to attach public IP to container",
					fmt.Sprintf("CETIC Cloud API error attaching IP %s to container %s: %s",
						planIPID, id, err.Error()),
				)
				return
			}
		}
	}

	// Re-fetch so state reflects the authoritative server view.
	fresh, err := r.client.GetContainer(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read container after update",
			fmt.Sprintf("Container %s was updated but the follow-up GET failed: %s", id, err.Error()),
		)
		return
	}
	diags := applyContainerToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	refreshPublicIPID(ctx, r.client, id, &plan.PublicIPID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// refreshPublicIPID resolves the UUID of the public IP currently attached
// to the container via `client.FindPublicIPByResource`. `GET /v1/containers/{id}`
// only returns the resolved IPv4 address, not the UUID.
func refreshPublicIPID(ctx context.Context, c *client.Client, ctID string, dst *types.String) {
	if c == nil || ctID == "" {
		return
	}
	ip, err := c.FindPublicIPByResource(ctx, "container", ctID)
	if err != nil {
		return
	}
	if ip == nil {
		*dst = types.StringNull()
		return
	}
	*dst = types.StringValue(ip.ID)
}

func (r *containerInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerInstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.DeleteContainer(ctx, id); err != nil {
		// Treat "already gone" as success — no point erroring on destroy when
		// the desired end state is already reached.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete container",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return
	}

	// Poll until GetContainer returns 404. If the timeout elapses, warn but
	// let Terraform remove the resource from state — CETIC Cloud is still
	// converging asynchronously and blocking the apply would be worse.
	pollErr := client.Poll(ctx, deletePollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetContainer(ctx, id)
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
			"Container deletion did not complete within the timeout",
			fmt.Sprintf("Container %s was scheduled for deletion but did not disappear "+
				"within %s: %s. Terraform will remove the resource from state; the Cloud "+
				"Lake backend should finish the teardown asynchronously.",
				id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing container with `terraform import
// ccp_container_instance.example <uuid>`. Read fills the rest.
func (r *containerInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// errPollTimeoutNoIP is a sentinel returned by pollUntilRunningWithIP when
// the container reached `running` but the IP address never resolved within
// the timeout. Distinct from a real polling error so the caller can downgrade
// it to a warning.
var errPollTimeoutNoIP = fmt.Errorf("container reached running state but IP address was not resolved within timeout")

// pollUntilRunningWithIP polls GetContainer every createPollInterval up to
// createPollTimeout, returning:
//   - nil if the container reaches `running` with a non-empty ip_address;
//   - errPollTimeoutNoIP if the container reaches `running` but the IP is
//     still unresolved when the timeout elapses;
//   - a non-nil error in every other failure case (status `error`, API error,
//     real polling timeout while still in `provisioning`, …).
func pollUntilRunningWithIP(ctx context.Context, c *client.Client, id string) error {
	deadline := time.Now().Add(createPollTimeout)
	reachedRunning := false
	var lastErrMsg string

	pollErr := client.Poll(ctx, createPollInterval, createPollTimeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetContainer(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case client.ContainerStatusError:
			if cur.ErrorMessage != nil {
				lastErrMsg = *cur.ErrorMessage
			}
			return false, fmt.Errorf("container %s entered error state during provisioning: %s", cur.ID, lastErrMsg)
		case client.ContainerStatusRunning:
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
		cur, err := c.GetContainer(ctx, id)
		if err == nil && (cur.IPAddress == nil || *cur.IPAddress == "") && cur.Status == client.ContainerStatusRunning {
			return errPollTimeoutNoIP
		}
	}
	return nil
}

// applyContainerToModel populates the framework model from the API
// representation. Always called after a successful Create/Read so state
// reflects the authoritative server view. Tags are normalised so a `nil` API
// response and an empty list both produce an empty list in state (avoids
// spurious diffs against an Optional+Computed list attribute).
func applyContainerToModel(ctx context.Context, src *client.Container, dst *containerInstanceResourceModel) diag.Diagnostics {
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
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))
	dst.UpdatedAt = types.StringValue(src.UpdatedAt.Format(time.RFC3339))

	dst.IPAddress = stringPtrToValue(src.IPAddress)
	dst.PublicIPAddress = stringPtrToValue(src.PublicIPAddress)
	dst.ScaleSetID = stringPtrToValue(src.ScaleSetID)
	dst.ErrorMessage = stringPtrToValue(src.ErrorMessage)

	// vnet_id is Optional (no Computed) — only mirror it back if the API
	// returned a value AND the planned attribute was set, so we don't
	// accidentally promote a null plan into a non-null state and trigger
	// drift on subsequent plans. If it was null in the plan, leave it null.
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
		// public_ip_id isn't returned directly by GET /v1/containers/{id};
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
