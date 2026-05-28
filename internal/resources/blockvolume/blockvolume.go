// Package blockvolume implements the ccp_block_volume Terraform
// resource.
//
// A block volume in CETIC Cloud is a Ceph RBD image that can be attached to
// either a container instance or a VM instance. Provisioning, deletion,
// attach, detach and resize are all asynchronous on the API side: the
// endpoints return 201/202 with a transient status (`creating`, `deleting`,
// `detaching`, …) and a Celery worker performs the actual rbd/qemu/pct
// operations. The provider therefore polls GetBlockVolume after every
// mutating call until the volume reaches a quiescent state.
//
// Mutability matrix (handled in Update):
//
//   - name              RequiresReplace (no rename API)
//   - region            RequiresReplace
//   - tags              RequiresReplace (no PATCH endpoint for tags)
//   - size_gb           grow only — shrinking is rejected with a diagnostic
//   - attached_to_id    mutable (attach / detach / re-attach)
//   - attached_to_type  mutable (carried alongside attached_to_id)
package blockvolume

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*blockVolumeResource)(nil)
	_ resource.ResourceWithConfigure   = (*blockVolumeResource)(nil)
	_ resource.ResourceWithImportState = (*blockVolumeResource)(nil)
)

// New returns a freshly-constructed ccp_block_volume resource.
// Wired in by provider.go via blockvolume.New.
func New() resource.Resource {
	return &blockVolumeResource{}
}

// blockVolumeResource is the framework Resource implementation. The client
// is stashed in Configure and reused by Create/Read/Update/Delete.
type blockVolumeResource struct {
	client *client.Client
}

// blockVolumeResourceModel mirrors the schema below 1-to-1. Tag names must
// match the schema attribute keys exactly.
type blockVolumeResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Region         types.String `tfsdk:"region"`
	SizeGB         types.Int64  `tfsdk:"size_gb"`
	AttachedToID   types.String `tfsdk:"attached_to_id"`
	AttachedToType types.String `tfsdk:"attached_to_type"`
	Tags           types.List   `tfsdk:"tags"`
	Status         types.String `tfsdk:"status"`
	AttachedToName types.String `tfsdk:"attached_to_name"`
	RBDPool        types.String `tfsdk:"rbd_pool"`
	RBDImage       types.String `tfsdk:"rbd_image"`
	ErrorMessage   types.String `tfsdk:"error_message"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscores
// and hyphens, 1..100 chars.
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,100}$`)

// uuidPattern is a permissive RFC 4122 matcher for CETIC Cloud resource IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Polling parameters.
//
//   - createPollTimeout: time we wait for `creating` → `available`.
//   - attachPollTimeout: time we wait for `available` → `attached`
//     and `attached/detaching` → `available`.
//   - resizePollTimeout: time we wait for the volume to leave its transient
//     state after a /resize call (status briefly flips before settling).
//   - deletePollTimeout: time we wait for the API to start returning 404.
const (
	pollInterval      = 5 * time.Second
	createPollTimeout = 90 * time.Second
	attachPollTimeout = 60 * time.Second
	resizePollTimeout = 60 * time.Second
	deletePollTimeout = 90 * time.Second
)

func (r *blockVolumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_block_volume"
}

func (r *blockVolumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud block volume (Ceph RBD). " +
			"Volumes can be attached to a container or VM instance via " +
			"`attached_to_id` + `attached_to_type`. The size can be grown in " +
			"place but never shrunk. Provisioning, attach, detach and resize " +
			"are asynchronous; the provider polls until the volume reaches a " +
			"quiescent state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the block volume.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Volume name (1–100 chars; alphanumerics, `_`, and `-`). " +
					"The API has no rename endpoint, so changes force replacement.",
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
					"`PAR` (Paris, France), or `ABJ` (Abidjan, Côte d'Ivoire).",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("RNN", "PAR", "ABJ"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size_gb": schema.Int64Attribute{
				MarkdownDescription: "Volume size in GB (1–16384). Mutable in place: " +
					"the API supports growing the volume via `/resize`. Shrinking " +
					"is rejected by the provider with a diagnostic — Ceph RBD " +
					"never shrinks safely.",
				Required: true,
				Validators: []validator.Int64{
					int64validator.Between(1, 16384),
				},
			},
			"attached_to_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the container or VM instance the volume is " +
					"currently attached to. Setting this attaches the volume; clearing it " +
					"detaches. Required to be paired with `attached_to_type`.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidPattern, "must be a valid UUID"),
				},
			},
			"attached_to_type": schema.StringAttribute{
				MarkdownDescription: "Type of the resource referenced by `attached_to_id`. " +
					"One of `container` or `vm`. Required when `attached_to_id` is set.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("container", "vm"),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the volume. The API has " +
					"no endpoint to mutate tags after creation, so changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle state. One of `creating`, " +
					"`available`, `attached`, `detaching`, `deleting`, or `error`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"attached_to_name": schema.StringAttribute{
				MarkdownDescription: "Display name of the resource the volume is attached to, " +
					"if any.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"rbd_pool": schema.StringAttribute{
				MarkdownDescription: "Ceph RBD pool that backs this volume (informational).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"rbd_image": schema.StringAttribute{
				MarkdownDescription: "Ceph RBD image name backing this volume (informational).",
				Computed:            true,
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
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the volume was created.",
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

func (r *blockVolumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *blockVolumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan blockVolumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Cross-field validation: attached_to_id and attached_to_type must be
	// either both null or both set.
	wantAttached, attachID, attachType, attachDiag := desiredAttachment(plan)
	if attachDiag != nil {
		resp.Diagnostics.Append(attachDiag)
		return
	}

	tags, diags := stringsFromList(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.BlockVolumeCreateRequest{
		Name:   plan.Name.ValueString(),
		Region: plan.Region.ValueString(),
		SizeGB: int(plan.SizeGB.ValueInt64()),
		Tags:   tags,
	}

	created, err := r.client.CreateBlockVolume(ctx, createReq)
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Block volume creation conflicts with current state",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create block volume",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	// Wait for `creating` → `available`. An immediate `error` short-circuits.
	if err := pollForStatus(ctx, r.client, created.ID, createPollTimeout, client.VolumeStatusAvailable); err != nil {
		resp.Diagnostics.AddError(
			"Block volume failed to reach available state",
			fmt.Sprintf("Volume %s did not reach `available` within %s: %s",
				created.ID, createPollTimeout, err.Error()),
		)
		return
	}

	// If the user asked for an attachment up-front, do it now. The volume
	// must be `available` first (we just polled for that). On error here we
	// surface a diagnostic but leave the volume in state — the user can
	// `terraform apply` again to retry the attach without re-creating.
	if wantAttached {
		if _, err := r.client.AttachBlockVolume(ctx, created.ID, client.BlockVolumeAttachRequest{
			ResourceID:   attachID,
			ResourceType: attachType,
		}); err != nil {
			if client.IsConflict(err) {
				resp.Diagnostics.AddError(
					"Block volume attachment conflicts with current state",
					fmt.Sprintf("CETIC Cloud rejected the attach call for volume %s: %s",
						created.ID, err.Error()),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Failed to attach block volume",
				fmt.Sprintf("Volume %s was created but the attach call failed: %s",
					created.ID, err.Error()),
			)
			return
		}
		if err := pollForStatus(ctx, r.client, created.ID, attachPollTimeout, client.VolumeStatusAttached); err != nil {
			resp.Diagnostics.AddError(
				"Block volume failed to reach attached state",
				fmt.Sprintf("Volume %s did not reach `attached` within %s: %s",
					created.ID, attachPollTimeout, err.Error()),
			)
			return
		}
	}

	// Re-fetch the authoritative record so timestamps, rbd_*, attached_to_name
	// and tags echo what the API has on file.
	fresh, err := r.client.GetBlockVolume(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read block volume after provisioning",
			fmt.Sprintf("Volume %s was created but the follow-up GET failed: %s",
				created.ID, err.Error()),
		)
		return
	}

	diags = applyVolumeToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blockVolumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blockVolumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetBlockVolume(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: volume was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read block volume",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s",
				state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we leave in state as-is — the next
	// refresh will pick up the eventual 404 and remove the resource.
	if got.Status == client.VolumeStatusDeleting {
		state.Status = types.StringValue(got.Status)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	diags := applyVolumeToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update handles the three mutable axes — size_gb, attached_to_id and
// attached_to_type — in this order: resize first (changes the underlying
// image while attachment, if any, stays put), then attachment changes.
func (r *blockVolumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state blockVolumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Validate the planned attachment shape up-front so we don't half-mutate
	// the volume before discovering an inconsistency.
	wantAttached, planAttachID, planAttachType, attachDiag := desiredAttachment(plan)
	if attachDiag != nil {
		resp.Diagnostics.Append(attachDiag)
		return
	}

	// 1. size_gb — grow only.
	planSize := plan.SizeGB.ValueInt64()
	stateSize := state.SizeGB.ValueInt64()
	switch {
	case planSize < stateSize:
		resp.Diagnostics.AddError(
			"Block volume cannot be shrunk",
			fmt.Sprintf("size_gb can only grow (current: %d, requested: %d). "+
				"Ceph RBD does not support safe shrinking; create a smaller "+
				"volume and copy the data manually if you need to reduce size.",
				stateSize, planSize),
		)
		return
	case planSize > stateSize:
		if _, err := r.client.ResizeBlockVolume(ctx, id, int(planSize)); err != nil {
			if client.IsConflict(err) {
				resp.Diagnostics.AddError(
					"Block volume resize conflicts with current state",
					fmt.Sprintf("CETIC Cloud rejected the resize call for volume %s: %s",
						id, err.Error()),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Failed to resize block volume",
				fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
			)
			return
		}
		// After a resize the volume should settle back to its prior steady
		// state (`available` if detached, `attached` if attached).
		settle := client.VolumeStatusAvailable
		if !state.AttachedToID.IsNull() && state.AttachedToID.ValueString() != "" {
			settle = client.VolumeStatusAttached
		}
		if err := pollForStatus(ctx, r.client, id, resizePollTimeout, settle); err != nil {
			resp.Diagnostics.AddError(
				"Block volume failed to settle after resize",
				fmt.Sprintf("Volume %s did not reach `%s` within %s after resize: %s",
					id, settle, resizePollTimeout, err.Error()),
			)
			return
		}
	}

	// 2. Attachment changes. Compute the four state/plan combinations.
	stateAttachID := ""
	if !state.AttachedToID.IsNull() && !state.AttachedToID.IsUnknown() {
		stateAttachID = state.AttachedToID.ValueString()
	}

	switch {
	case stateAttachID == "" && wantAttached:
		// detach → attach (was already detached, just attach).
		if err := r.attachAndPoll(ctx, id, planAttachID, planAttachType); err != nil {
			resp.Diagnostics.Append(err...)
			return
		}
	case stateAttachID != "" && !wantAttached:
		// attached → detach.
		if err := r.detachAndPoll(ctx, id); err != nil {
			resp.Diagnostics.Append(err...)
			return
		}
	case stateAttachID != "" && wantAttached && stateAttachID != planAttachID:
		// Re-attach to a different target: detach first, then attach.
		if err := r.detachAndPoll(ctx, id); err != nil {
			resp.Diagnostics.Append(err...)
			return
		}
		if err := r.attachAndPoll(ctx, id, planAttachID, planAttachType); err != nil {
			resp.Diagnostics.Append(err...)
			return
		}
	}
	// All other cases (no attachment changes, or same target — possibly only
	// attached_to_type changed which the API doesn't support without a
	// detach/attach cycle, but we treat it as a no-op since the type is
	// derived from the attached resource itself) require no action.

	// Re-fetch and project onto state.
	fresh, err := r.client.GetBlockVolume(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read block volume after update",
			fmt.Sprintf("Volume %s was updated but the follow-up GET failed: %s",
				id, err.Error()),
		)
		return
	}

	diags := applyVolumeToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blockVolumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state blockVolumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// If the volume is currently attached, detach it first. Refusing to
	// delete an attached volume protects the user from a 409 from the API,
	// which would leave the resource half-managed.
	if state.Status.ValueString() == client.VolumeStatusAttached {
		if diags := r.detachAndPoll(ctx, id); diags != nil {
			resp.Diagnostics.AddError(
				"Failed to detach block volume before deletion",
				fmt.Sprintf("Volume %s is currently attached and could not be detached "+
					"automatically. Detach it manually then re-run `terraform destroy`. "+
					"Underlying error: %s", id, diagsToString(diags)),
			)
			return
		}
	}

	if err := r.client.DeleteBlockVolume(ctx, id); err != nil {
		// Treat "already gone" as success.
		if client.IsNotFound(err) {
			return
		}
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Block volume deletion conflicts with current state",
				fmt.Sprintf("CETIC Cloud refused to delete volume %s (likely still attached): %s",
					id, err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete block volume",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return
	}

	// Poll for 404. If we time out, warn but let Terraform drop the resource;
	// the backend is converging asynchronously.
	pollErr := client.Poll(ctx, pollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetBlockVolume(ctx, id)
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
			"Block volume deletion did not complete within the timeout",
			fmt.Sprintf("Volume %s was scheduled for deletion but did not disappear "+
				"within %s: %s. Terraform will remove the resource from state; the Cloud "+
				"Lake backend should finish the teardown asynchronously.",
				id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing volume with `terraform import
// ccp_block_volume.example <uuid>`. Read fills the rest.
func (r *blockVolumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// attachAndPoll performs an attach call followed by a poll until the volume
// reaches `attached`. Returns nil diagnostics on success, otherwise a slice
// suitable for resp.Diagnostics.Append.
func (r *blockVolumeResource) attachAndPoll(ctx context.Context, id, resourceID, resourceType string) diag.Diagnostics {
	var diags diag.Diagnostics
	if _, err := r.client.AttachBlockVolume(ctx, id, client.BlockVolumeAttachRequest{
		ResourceID:   resourceID,
		ResourceType: resourceType,
	}); err != nil {
		if client.IsConflict(err) {
			diags.AddError(
				"Block volume attachment conflicts with current state",
				fmt.Sprintf("CETIC Cloud rejected the attach call for volume %s: %s",
					id, err.Error()),
			)
			return diags
		}
		diags.AddError(
			"Failed to attach block volume",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return diags
	}
	if err := pollForStatus(ctx, r.client, id, attachPollTimeout, client.VolumeStatusAttached); err != nil {
		diags.AddError(
			"Block volume failed to reach attached state",
			fmt.Sprintf("Volume %s did not reach `attached` within %s: %s",
				id, attachPollTimeout, err.Error()),
		)
		return diags
	}
	return nil
}

// detachAndPoll performs a detach call followed by a poll until the volume
// reaches `available`. Returns nil diagnostics on success, otherwise a slice
// suitable for resp.Diagnostics.Append.
func (r *blockVolumeResource) detachAndPoll(ctx context.Context, id string) diag.Diagnostics {
	var diags diag.Diagnostics
	if _, err := r.client.DetachBlockVolume(ctx, id); err != nil {
		if client.IsConflict(err) {
			diags.AddError(
				"Block volume detach conflicts with current state",
				fmt.Sprintf("CETIC Cloud rejected the detach call for volume %s: %s",
					id, err.Error()),
			)
			return diags
		}
		diags.AddError(
			"Failed to detach block volume",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return diags
	}
	if err := pollForStatus(ctx, r.client, id, attachPollTimeout, client.VolumeStatusAvailable); err != nil {
		diags.AddError(
			"Block volume failed to reach available state after detach",
			fmt.Sprintf("Volume %s did not reach `available` within %s: %s",
				id, attachPollTimeout, err.Error()),
		)
		return diags
	}
	return nil
}

// pollForStatus polls GetBlockVolume every pollInterval up to timeout,
// stopping when the volume reaches `target`. Reaching `error` is treated as
// a hard failure and surfaces error_message. The 404 case is also a hard
// failure here — the caller should not be polling for a deleted volume.
func pollForStatus(ctx context.Context, c *client.Client, id string, timeout time.Duration, target string) error {
	return client.Poll(ctx, pollInterval, timeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetBlockVolume(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case target:
			return true, nil
		case client.VolumeStatusError:
			msg := ""
			if cur.ErrorMessage != nil {
				msg = *cur.ErrorMessage
			}
			return false, fmt.Errorf("volume %s entered error state: %s", cur.ID, msg)
		default:
			return false, nil
		}
	})
}

// desiredAttachment inspects the plan and returns whether the user wants the
// volume attached, plus the (id, type) pair if so. Surfaces a diagnostic
// when the two attached_to_* fields are mis-paired (only one of the two set).
func desiredAttachment(m blockVolumeResourceModel) (bool, string, string, *diag.ErrorDiagnostic) {
	idSet := !m.AttachedToID.IsNull() && !m.AttachedToID.IsUnknown() && m.AttachedToID.ValueString() != ""
	typeSet := !m.AttachedToType.IsNull() && !m.AttachedToType.IsUnknown() && m.AttachedToType.ValueString() != ""

	switch {
	case idSet && typeSet:
		return true, m.AttachedToID.ValueString(), m.AttachedToType.ValueString(), nil
	case !idSet && !typeSet:
		return false, "", "", nil
	default:
		d := diag.NewErrorDiagnostic(
			"Inconsistent block volume attachment configuration",
			"`attached_to_id` and `attached_to_type` must either both be set or "+
				"both be omitted. Set both to attach the volume, or remove both to "+
				"leave it detached.",
		)
		return false, "", "", &d
	}
}

// applyVolumeToModel populates the framework model from the API
// representation. Always called after a successful Create/Read/Update so
// state reflects the authoritative server view. Tags are normalised so a
// `nil` API response and an empty list both produce an empty list in state
// (avoids spurious diffs against an Optional+Computed list attribute).
func applyVolumeToModel(ctx context.Context, src *client.BlockVolume, dst *blockVolumeResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.Name = types.StringValue(src.Name)
	dst.Region = types.StringValue(src.Region)
	dst.SizeGB = types.Int64Value(int64(src.SizeGB))
	dst.Status = types.StringValue(src.Status)
	dst.AttachedToID = stringPtrToValue(src.AttachedToID)
	dst.AttachedToType = stringPtrToValue(src.AttachedToType)
	dst.AttachedToName = stringPtrToValue(src.AttachedToName)
	dst.RBDPool = stringPtrToValue(src.RBDPool)
	dst.RBDImage = stringPtrToValue(src.RBDImage)
	dst.ErrorMessage = stringPtrToValue(src.ErrorMessage)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))
	dst.UpdatedAt = types.StringValue(src.UpdatedAt.Format(time.RFC3339))

	tagValues := make([]string, 0, len(src.Tags))
	tagValues = append(tagValues, src.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	dst.Tags = tagsList
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

// stringsFromList converts the framework List representation into a Go
// slice. Null and unknown both collapse to nil so callers can hand the
// result straight to the API client.
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

// diagsToString flattens a diag.Diagnostics into a single human-readable
// line, used to compose nested error messages where we want to surface the
// underlying detail without losing it inside a sub-diagnostic.
func diagsToString(diags diag.Diagnostics) string {
	if len(diags) == 0 {
		return ""
	}
	out := ""
	for i, d := range diags {
		if i > 0 {
			out += "; "
		}
		out += d.Summary()
		if d.Detail() != "" {
			out += ": " + d.Detail()
		}
	}
	return out
}
