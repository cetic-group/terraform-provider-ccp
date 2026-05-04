// Package publicip implements the ccp_public_ip Terraform resource.
//
// A public IP in Cloud Lake is allocated from a per-region pool (either a
// classic OPNsense/Orange pool or an `ipaas_routed` pool that announces a
// BYOIP prefix via FRR/BGP from a Scaleway edge). Allocation and release are
// synchronous — the API returns 201 with the full PublicIP record and 204 on
// release. Attach/detach are synchronous for demo pools but dispatch a Celery
// task for IPaaS pools, so the provider polls GetPublicIP after every
// attach/detach until the IP reaches a quiescent status.
//
// User intent for attachment is expressed via `attached_to_id` +
// `attached_to_type`. Both nil → IP should be `allocated` (not attached). Both
// set → IP should be `attached` to that target. Load-balancer attachment uses
// a different API path (POST /load-balancers/{id}/attach-ip) and is therefore
// surfaced read-only here: the provider warns if the user tries to set
// `attached_to_type=load_balancer` and refuses the attach.
package publicip

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*publicIPResource)(nil)
	_ resource.ResourceWithConfigure   = (*publicIPResource)(nil)
	_ resource.ResourceWithImportState = (*publicIPResource)(nil)
)

// New returns a freshly-constructed ccp_public_ip resource. Wired in by
// provider.go via publicip.New.
func New() resource.Resource {
	return &publicIPResource{}
}

// publicIPResource is the framework Resource implementation. The client is
// stashed in Configure and reused by Create/Read/Update/Delete.
type publicIPResource struct {
	client *client.Client
}

// publicIPResourceModel mirrors the schema below 1-to-1. Tag names must match
// the schema attribute keys exactly.
type publicIPResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Region           types.String `tfsdk:"region"`
	PoolID           types.String `tfsdk:"pool_id"`
	AttachedToID     types.String `tfsdk:"attached_to_id"`
	AttachedToType   types.String `tfsdk:"attached_to_type"`
	IPAddress        types.String `tfsdk:"ip_address"`
	Status           types.String `tfsdk:"status"`
	ContainerID      types.String `tfsdk:"container_id"`
	VMInstanceID     types.String `tfsdk:"vm_instance_id"`
	LoadBalancerID   types.String `tfsdk:"load_balancer_id"`
	LoadBalancerName types.String `tfsdk:"load_balancer_name"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

// uuidPattern is a permissive RFC 4122 matcher for Cloud Lake resource IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Polling parameters.
//
//   - attachPollTimeout: time we wait for `allocated` → `attached` (or vice
//     versa on detach). IPaaS attaches dispatch a Celery task that takes a
//     few seconds end-to-end (DNAT entry on the edge + route + prefix-list
//     refresh + secondary IP injection on the target).
const (
	pollInterval      = 5 * time.Second
	attachPollTimeout = 60 * time.Second
)

func (r *publicIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_ip"
}

func (r *publicIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Cloud Lake public IP address. The IP is " +
			"allocated from a region pool (classic Orange/OPNsense pool or an " +
			"`ipaas_routed` BYOIP pool), and can be attached to a container or " +
			"VM instance via `attached_to_id` + `attached_to_type`. Load-balancer " +
			"attachment is handled by `ccp_load_balancer` and is surfaced " +
			"here read-only.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the public IP allocation.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Cloud Lake region. One of `RNN` (Rennes, France), " +
					"`PAR` (Paris, France), or `ABJ` (Abidjan, Côte d'Ivoire). " +
					"Forces replacement on change.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("RNN", "PAR", "ABJ"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"pool_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the IP pool to allocate from. If omitted, " +
					"the API picks the first available pool in the region. Forces " +
					"replacement on change.",
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
			"attached_to_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the container or VM instance the IP should be " +
					"attached to. Setting this attaches the IP; clearing it detaches. " +
					"Required to be paired with `attached_to_type`. Load-balancer " +
					"attachment uses a different code path and cannot be expressed here.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidPattern, "must be a valid UUID"),
				},
			},
			"attached_to_type": schema.StringAttribute{
				MarkdownDescription: "Type of the resource referenced by `attached_to_id`. " +
					"One of `container` or `vm_instance`. Required when `attached_to_id` " +
					"is set. `load_balancer` is intentionally not accepted — use the " +
					"`ccp_load_balancer` resource's IP attachment instead.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("container", "vm_instance"),
				},
			},
			"ip_address": schema.StringAttribute{
				MarkdownDescription: "The actual IPv4 address assigned by the pool.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle state. One of `available`, " +
					"`allocated`, `attached`, or `reserved`. `reserved` is a CETIC-managed " +
					"lock and prevents release.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"container_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the container this IP is attached to, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vm_instance_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VM instance this IP is attached to, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"load_balancer_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the load balancer this IP is attached to, if any. " +
					"Read-only — set via the load-balancer resource.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"load_balancer_name": schema.StringAttribute{
				MarkdownDescription: "Display name of the load balancer this IP is attached to, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the IP was allocated.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *publicIPResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *publicIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicIPResourceModel
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

	allocReq := client.PublicIPAllocateRequest{
		Region: plan.Region.ValueString(),
	}
	if !plan.PoolID.IsNull() && !plan.PoolID.IsUnknown() && plan.PoolID.ValueString() != "" {
		v := plan.PoolID.ValueString()
		allocReq.PoolID = &v
	}

	created, err := r.client.AllocatePublicIP(ctx, allocReq)
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Public IP allocation conflicts with current state",
				fmt.Sprintf("Cloud Lake rejected the allocate call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to allocate public IP",
			fmt.Sprintf("Cloud Lake API error: %s", err.Error()),
		)
		return
	}

	// Allocate is synchronous — `created` already carries the full state. If
	// the user asked for an attachment up-front, do it now.
	if wantAttached {
		if diags := r.attachAndPoll(ctx, created.ID, attachID, attachType); diags != nil {
			resp.Diagnostics.Append(diags...)
			return
		}
	}

	// Re-fetch the authoritative record so timestamps and any IPaaS-task
	// side-effects are reflected.
	fresh, err := r.client.GetPublicIP(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read public IP after allocation",
			fmt.Sprintf("IP %s was allocated but the follow-up GET failed: %s",
				created.ID, err.Error()),
		)
		return
	}

	applyPublicIPToModel(fresh, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetPublicIP(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: IP was released out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read public IP",
			fmt.Sprintf("Cloud Lake API error for id %s: %s",
				state.ID.ValueString(), err.Error()),
		)
		return
	}

	// If a load balancer owns the attachment, surface a warning so the user
	// notices the drift between their (container/vm_instance) plan and reality.
	if got.LoadBalancerID != nil {
		resp.Diagnostics.AddWarning(
			"Public IP is attached to a load balancer",
			fmt.Sprintf("Public IP %s is currently attached to load balancer %s. "+
				"Load-balancer attachment is managed by the ccp_load_balancer "+
				"resource, not ccp_public_ip. The `attached_to_id` / "+
				"`attached_to_type` attributes will appear empty in state.",
				got.ID, derefString(got.LoadBalancerID)),
		)
	}

	applyPublicIPToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update handles attach/detach/re-attach transitions. region and pool_id force
// replacement, so they cannot reach this path.
func (r *publicIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state publicIPResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Validate the planned attachment shape up-front.
	wantAttached, planAttachID, planAttachType, attachDiag := desiredAttachment(plan)
	if attachDiag != nil {
		resp.Diagnostics.Append(attachDiag)
		return
	}

	stateAttachID := ""
	stateAttachType := ""
	if !state.AttachedToID.IsNull() && !state.AttachedToID.IsUnknown() {
		stateAttachID = state.AttachedToID.ValueString()
	}
	if !state.AttachedToType.IsNull() && !state.AttachedToType.IsUnknown() {
		stateAttachType = state.AttachedToType.ValueString()
	}

	switch {
	case stateAttachID == "" && !wantAttached:
		// No-op: was detached, stays detached.
	case stateAttachID != "" && wantAttached &&
		stateAttachID == planAttachID && stateAttachType == planAttachType:
		// No-op: same target and same type.
	case stateAttachID == "" && wantAttached:
		// detach → attach (was already detached, just attach).
		if diags := r.attachAndPoll(ctx, id, planAttachID, planAttachType); diags != nil {
			resp.Diagnostics.Append(diags...)
			return
		}
	case stateAttachID != "" && !wantAttached:
		// attached → detach.
		if diags := r.detachAndPoll(ctx, id); diags != nil {
			resp.Diagnostics.Append(diags...)
			return
		}
	default:
		// Re-attach to a different target (or different type): detach then attach.
		if diags := r.detachAndPoll(ctx, id); diags != nil {
			resp.Diagnostics.Append(diags...)
			return
		}
		if diags := r.attachAndPoll(ctx, id, planAttachID, planAttachType); diags != nil {
			resp.Diagnostics.Append(diags...)
			return
		}
	}

	// Re-fetch and project onto state.
	fresh, err := r.client.GetPublicIP(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read public IP after update",
			fmt.Sprintf("IP %s was updated but the follow-up GET failed: %s",
				id, err.Error()),
		)
		return
	}

	applyPublicIPToModel(fresh, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicIPResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// CETIC-locked IPs cannot be released by tenants.
	if state.Status.ValueString() == client.PublicIPStatusReserved {
		resp.Diagnostics.AddError(
			"Cannot release a reserved public IP",
			fmt.Sprintf("Public IP %s is in `reserved` state — these are locked by CETIC "+
				"and cannot be released by tenants. Contact support to unlock the IP "+
				"before destroying it from Terraform.", id),
		)
		return
	}

	// If currently attached, detach first to avoid a 409 on release.
	if state.Status.ValueString() == client.PublicIPStatusAttached {
		if diags := r.detachAndPoll(ctx, id); diags != nil {
			resp.Diagnostics.AddError(
				"Failed to detach public IP before release",
				fmt.Sprintf("Public IP %s is currently attached and could not be detached "+
					"automatically. Detach it manually then re-run `terraform destroy`. "+
					"Underlying error: %s", id, diagsToString(diags)),
			)
			return
		}
	}

	if err := r.client.ReleasePublicIP(ctx, id); err != nil {
		// Treat "already gone" as success.
		if client.IsNotFound(err) {
			return
		}
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Public IP release conflicts with current state",
				fmt.Sprintf("Cloud Lake refused to release IP %s (likely still attached): %s",
					id, err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to release public IP",
			fmt.Sprintf("Cloud Lake API error for id %s: %s", id, err.Error()),
		)
		return
	}
	// Release returns 204 synchronously — no polling required.
}

// ImportState lets users adopt an existing public IP with `terraform import
// ccp_public_ip.example <uuid>`. Read fills the rest.
func (r *publicIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// attachAndPoll performs an attach call followed by a poll until the IP
// reaches `attached`. IPaaS pools dispatch a Celery task and stay in
// `allocated` until it completes; demo pools transition synchronously.
// Returns nil diagnostics on success, otherwise a slice suitable for
// resp.Diagnostics.Append.
func (r *publicIPResource) attachAndPoll(ctx context.Context, id, resourceID, resourceType string) diag.Diagnostics {
	var diags diag.Diagnostics
	if _, err := r.client.AttachPublicIP(ctx, id, client.PublicIPAttachRequest{
		ResourceType: resourceType,
		ResourceID:   resourceID,
	}); err != nil {
		if client.IsConflict(err) {
			diags.AddError(
				"Public IP attachment conflicts with current state",
				fmt.Sprintf("Cloud Lake rejected the attach call for IP %s: %s. "+
					"This usually means the IP is already attached, or the target "+
					"resource already has a public IP attached.",
					id, err.Error()),
			)
			return diags
		}
		diags.AddError(
			"Failed to attach public IP",
			fmt.Sprintf("Cloud Lake API error for id %s: %s. Note that IPaaS-routed "+
				"pools require the target VNet to have `snat=false` (a 422 here "+
				"surfaces that mismatch).", id, err.Error()),
		)
		return diags
	}
	if err := pollForAttached(ctx, r.client, id); err != nil {
		diags.AddError(
			"Public IP failed to reach attached state",
			fmt.Sprintf("IP %s did not reach `attached` within %s: %s",
				id, attachPollTimeout, err.Error()),
		)
		return diags
	}
	return nil
}

// detachAndPoll performs a detach call followed by a poll until the IP returns
// to `allocated`. Returns nil diagnostics on success.
func (r *publicIPResource) detachAndPoll(ctx context.Context, id string) diag.Diagnostics {
	var diags diag.Diagnostics
	if _, err := r.client.DetachPublicIP(ctx, id); err != nil {
		if client.IsConflict(err) {
			diags.AddError(
				"Public IP detach conflicts with current state",
				fmt.Sprintf("Cloud Lake rejected the detach call for IP %s: %s",
					id, err.Error()),
			)
			return diags
		}
		diags.AddError(
			"Failed to detach public IP",
			fmt.Sprintf("Cloud Lake API error for id %s: %s", id, err.Error()),
		)
		return diags
	}
	if err := pollForAllocated(ctx, r.client, id); err != nil {
		diags.AddError(
			"Public IP failed to reach allocated state after detach",
			fmt.Sprintf("IP %s did not reach `allocated` within %s: %s",
				id, attachPollTimeout, err.Error()),
		)
		return diags
	}
	return nil
}

// pollForAttached polls GetPublicIP every pollInterval up to attachPollTimeout,
// stopping when the IP reaches `attached`. `allocated` is treated as
// "still in flight" (IPaaS Celery task still running). Anything else is a
// hard failure.
func pollForAttached(ctx context.Context, c *client.Client, id string) error {
	return client.Poll(ctx, pollInterval, attachPollTimeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetPublicIP(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case client.PublicIPStatusAttached:
			return true, nil
		case client.PublicIPStatusAllocated:
			// IPaaS task still in flight — keep polling.
			return false, nil
		default:
			return false, fmt.Errorf("public IP %s entered unexpected state %q while waiting for `attached`",
				cur.ID, cur.Status)
		}
	})
}

// pollForAllocated polls GetPublicIP every pollInterval up to attachPollTimeout,
// stopping when the IP reaches `allocated` (i.e. detached). `attached` is
// treated as "still in flight" (Celery task still tearing down DNAT/route).
// Anything else is a hard failure.
func pollForAllocated(ctx context.Context, c *client.Client, id string) error {
	return client.Poll(ctx, pollInterval, attachPollTimeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetPublicIP(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case client.PublicIPStatusAllocated:
			return true, nil
		case client.PublicIPStatusAttached:
			// Detach task still in flight — keep polling.
			return false, nil
		default:
			return false, fmt.Errorf("public IP %s entered unexpected state %q while waiting for `allocated`",
				cur.ID, cur.Status)
		}
	})
}

// desiredAttachment inspects the plan and returns whether the user wants the
// IP attached, plus the (id, type) pair if so. Surfaces a diagnostic when the
// two attached_to_* fields are mis-paired (only one of the two set).
func desiredAttachment(m publicIPResourceModel) (bool, string, string, *diag.ErrorDiagnostic) {
	idSet := !m.AttachedToID.IsNull() && !m.AttachedToID.IsUnknown() && m.AttachedToID.ValueString() != ""
	typeSet := !m.AttachedToType.IsNull() && !m.AttachedToType.IsUnknown() && m.AttachedToType.ValueString() != ""

	switch {
	case idSet && typeSet:
		return true, m.AttachedToID.ValueString(), m.AttachedToType.ValueString(), nil
	case !idSet && !typeSet:
		return false, "", "", nil
	default:
		d := diag.NewErrorDiagnostic(
			"Inconsistent public IP attachment configuration",
			"`attached_to_id` and `attached_to_type` must either both be set or "+
				"both be omitted. Set both to attach the IP, or remove both to "+
				"leave it allocated but detached.",
		)
		return false, "", "", &d
	}
}

// applyPublicIPToModel populates the framework model from the API
// representation. Always called after a successful Create/Read/Update so state
// reflects the authoritative server view. The user-facing `attached_to_id` /
// `attached_to_type` are derived from the typed back-references: container
// wins over vm_instance over load_balancer (the latter is read-only and
// surfaced via a Read warning rather than a settable type).
func applyPublicIPToModel(src *client.PublicIP, dst *publicIPResourceModel) {
	dst.ID = types.StringValue(src.ID)
	dst.PoolID = types.StringValue(src.PoolID)
	dst.Region = types.StringValue(src.Region)
	dst.IPAddress = types.StringValue(src.IPAddress)
	dst.Status = types.StringValue(src.Status)
	dst.ContainerID = stringPtrToValue(src.ContainerID)
	dst.VMInstanceID = stringPtrToValue(src.VMInstanceID)
	dst.LoadBalancerID = stringPtrToValue(src.LoadBalancerID)
	dst.LoadBalancerName = stringPtrToValue(src.LoadBalancerName)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))

	switch {
	case src.ContainerID != nil:
		dst.AttachedToID = types.StringValue(*src.ContainerID)
		dst.AttachedToType = types.StringValue("container")
	case src.VMInstanceID != nil:
		dst.AttachedToID = types.StringValue(*src.VMInstanceID)
		dst.AttachedToType = types.StringValue("vm_instance")
	default:
		// LoadBalancer-owned attachments are intentionally not mirrored into
		// the user-settable fields — Read surfaces a warning instead. Anything
		// else (including the LB case) leaves both attributes null so the
		// user's plan-with-no-attachment doesn't perma-diff.
		dst.AttachedToID = types.StringNull()
		dst.AttachedToType = types.StringNull()
	}
}

// stringPtrToValue collapses a *string into a framework String value: nil →
// Null, otherwise the underlying string.
func stringPtrToValue(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// derefString returns the underlying string of a non-nil *string, or "" when
// nil. Used inside diagnostics where the caller has already ruled out nil.
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// diagsToString flattens a diag.Diagnostics into a single human-readable line,
// used to compose nested error messages where we want to surface the
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
