// Package vpc implements the ccp_vpc Terraform resource.
//
// A VPC in CETIC Cloud is an isolated private network (its gateway is
// provisioned lazily on first VNet creation). `name`, `region`, `cidr` and
// `tags` force replacement on change. Only `dedicated_egress_ip` is updated in
// place, through PATCH /v1/vpcs/{id}.
//
// Dedicated egress IP (cetic-cloud-platform#1734): with
// `dedicated_egress_ip = true` the platform picks a public IP and makes the
// `snat = true` VNets of the VPC go out through it. Setting it up is
// asynchronous: `egress_ip_status` goes `attaching` → `attached`. It stays
// `allocated` (reserved, not served yet) while the VPC has no `snat = true`
// VNet, which is always the case right after the VPC is created. Setting it
// back to false keeps the `egress_*` attributes filled (`detaching`) until the
// platform has proven the removal, then they become null. Create and Update
// wait for the egress IP to settle (see classifyEgressPoll).
//
// Provisioning is asynchronous: POST /v1/vpcs returns 201 immediately but the
// network may still be propagating. Status transitions are
// `active` (terminal success) or `error` (terminal failure). We poll
// GetVPC for up to 90 s and re-fetch the resource once it settles so the
// Terraform state reflects the authoritative server view (status,
// timestamps).
//
// Deletion is also asynchronous: the VPC enters `deleting` and disappears
// from the API once the network teardown completes. We poll
// for 404 up to 60 s and surface a warning rather than an error if the
// timeout elapses, since the resource will still be removed from Terraform
// state and the API is converging on its own.
//
// The DELETE itself can transiently return 409 "Le VPC contient des VNets"
// during a `terraform destroy`: each child VNet's Delete already polls until it
// leaves the per-VPC VNet list (404), but the backend network cleanup that clears the VPC-level "has vnets" precondition
// finishes a beat later. We retry the DELETE on 409 until that precondition
// clears (up to deleteConflictRetryTimeout) instead of failing the apply and
// forcing the user to re-run destroy. A 409 that outlives the budget (a child
// genuinely stuck) is surfaced verbatim so it stays actionable.
package vpc

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*vpcResource)(nil)
	_ resource.ResourceWithConfigure   = (*vpcResource)(nil)
	_ resource.ResourceWithImportState = (*vpcResource)(nil)
)

// New returns a freshly-constructed ccp_vpc resource. Wired in by
// provider.go via vpc.New.
func New() resource.Resource {
	return &vpcResource{}
}

// vpcResource is the framework Resource implementation. The client is stashed
// in Configure and reused by Create/Read/Update/Delete.
type vpcResource struct {
	client *client.Client
}

// vpcResourceModel mirrors the schema below 1-to-1. Tag names must match the
// schema attribute keys exactly.
type vpcResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Region    types.String `tfsdk:"region"`
	Cidr      types.String `tfsdk:"cidr"`
	Tags      types.List   `tfsdk:"tags"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`

	DedicatedEgressIP types.Bool   `tfsdk:"dedicated_egress_ip"`
	EgressIP          types.String `tfsdk:"egress_ip"`
	EgressIPType      types.String `tfsdk:"egress_ip_type"`
	EgressIPStatus    types.String `tfsdk:"egress_ip_status"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscores
// and hyphens (length enforced separately).
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Polling parameters — Create waits up to 90 s for the VPC to leave the
// transitional state, Delete waits up to 60 s for the resource to disappear.
const (
	createPollInterval = 5 * time.Second
	createPollTimeout  = 90 * time.Second
	deletePollInterval = 5 * time.Second
	deletePollTimeout  = 60 * time.Second
	// deleteConflictRetryTimeout bounds how long we keep retrying the DELETE
	// while it returns 409 (child VNet teardown still settling server-side).
	// Larger than deletePollTimeout to give the backend network
	// cleanup headroom in busy regions before we give up and surface the 409.
	deleteConflictRetryTimeout = 120 * time.Second
)

// egressPollTimeout bounds the wait for the dedicated egress IP to settle
// (set up or removed). The platform runs the change from a background task
// that executes commands on the VPC gateway (up to 3 minutes each) and has to
// prove the result, so the budget is wider than for the VPC itself. Vars, not
// consts, so unit tests can shorten them.
var (
	egressPollInterval = 5 * time.Second
	egressPollTimeout  = 10 * time.Minute
)

func (r *vpcResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vpc"
}

func (r *vpcResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VPC. A VPC is a regional layer-3 boundary " +
			"(an isolated private network with an optional per-VPC NAT gateway). Any change to " +
			"`name`, `region`, `cidr` or `tags` forces replacement; `dedicated_egress_ip` is " +
			"updated in place. Creation is asynchronous: the provider polls until the VPC reaches " +
			"the `active` state (or up to 90 seconds).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the VPC.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "VPC name (max 100 chars; alphanumerics, `_`, and `-`).",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(100),
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must contain only letters, digits, underscores, or hyphens",
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
			"cidr": schema.StringAttribute{
				MarkdownDescription: "Private address block of the VPC (RFC1918, `/16` to `/24` mask). " +
					"Auto-allocated by the platform when omitted. VNets created inside the VPC must be " +
					"sub-ranges of this block. Immutable after creation — changing it forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the VPC. The API has no " +
					"endpoint to mutate tags after creation, so changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle state. One of `active`, `deleting`, or " +
					"`error`. After a successful apply this will always be `active`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the VPC was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			// An explicit `false` default, not Optional+Computed alone: with
			// no default, removing the attribute from the configuration would
			// keep the state value (true) forever instead of switching off.
			"dedicated_egress_ip": schema.BoolAttribute{
				MarkdownDescription: "Route the internet traffic of the VPC's `snat = true` VNets " +
					"through a dedicated public IP picked by the platform, instead of the shared " +
					"egress. Defaults to `false`. Updated in place; setting it back to `false` " +
					"(or removing it) releases the IP once the platform has proven it is no " +
					"longer used. The IP counts against the `max_public_ips` quota.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"egress_ip": schema.StringAttribute{
				MarkdownDescription: "Dedicated egress public IP of the VPC (to give to partners " +
					"that filter by source address). `null` while no IP is linked.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					useStateUnlessEgressChanges(),
				},
			},
			"egress_ip_type": schema.StringAttribute{
				MarkdownDescription: "Kind of the dedicated egress IP: `edge`, `firewall` or `nat`. " +
					"`null` while no IP is linked.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					useStateUnlessEgressChanges(),
				},
			},
			"egress_ip_status": schema.StringAttribute{
				MarkdownDescription: "Status of the dedicated egress IP: `allocated` (reserved, " +
					"served once the VPC has a `snat = true` VNet), `attaching`, `attached` (in " +
					"service), `detaching` or `error`. `null` while no IP is linked.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					useStateUnlessEgressChanges(),
				},
			},
		},
	}
}

func (r *vpcResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *vpcResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Materialise tags from the framework List into a plain []string. A null
	// or unknown list collapses to nil, which the API treats as "no tags".
	tags, diags := tagsFromList(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Pass the CIDR only when the user set it; nil tells the API to
	// auto-allocate a /16. ValueStringPointer yields nil for null/unknown.
	var cidr *string
	if !plan.Cidr.IsNull() && !plan.Cidr.IsUnknown() {
		cidr = plan.Cidr.ValueStringPointer()
	}

	wantEgress := plan.DedicatedEgressIP.ValueBool()
	created, err := r.client.CreateVPC(ctx, client.VPCCreateRequest{
		Name:              plan.Name.ValueString(),
		Region:            plan.Region.ValueString(),
		CIDR:              cidr,
		Tags:              tags,
		DedicatedEgressIP: wantEgress,
	})
	if err != nil {
		// 409 (e.g. name collision, no free egress IP), 422 (dedicated egress
		// not offered), 429 (public IP quota) and 503 (no capacity available
		// in the region) all arrive with a detail message we want to surface
		// verbatim — the API speaks French and the messages are user-facing.
		// The platform picks the egress IP before creating anything, so a
		// refusal leaves no VPC behind.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VPC creation refused (conflict)",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create VPC",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	// The remote object exists from here on. Persist it before waiting: if
	// provisioning fails, Terraform keeps the resource (tainted) and replaces it
	// on the next apply. Dropped from state it would be neither destroyed nor
	// re-planned, and the next apply would strand it and create a second one.
	// The projection runs on a COPY: `plan` still carries the practitioner's
	// intent, which later code reads to decide follow-up calls (VNet isolation,
	// public IP attach). Overwriting it here would silently drop that intent.
	early := plan
	resp.Diagnostics.Append(applyVPCToModel(ctx, created, &early)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &early)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Fast path: the API may already report `active` on the initial response.
	// Otherwise poll until the status settles.
	final := created
	switch created.Status {
	case client.StatusActive:
		// Done — no extra round-trip needed.
	case client.StatusError:
		resp.Diagnostics.AddError(
			"VPC entered error state during provisioning",
			fmt.Sprintf("VPC %s reported status `error` immediately after creation. "+
				"Check the CETIC Cloud console or backoffice for the underlying cause.", created.ID),
		)
		return
	default:
		pollErr := client.Poll(ctx, createPollInterval, createPollTimeout, func(ctx context.Context) (bool, error) {
			cur, err := r.client.GetVPC(ctx, created.ID)
			if err != nil {
				return false, err
			}
			switch cur.Status {
			case client.StatusError:
				return false, fmt.Errorf("VPC %s entered error state during provisioning", cur.ID)
			case client.StatusActive:
				return true, nil
			default:
				return false, nil
			}
		})
		if pollErr != nil {
			resp.Diagnostics.AddError(
				"VPC failed to reach active state",
				fmt.Sprintf("CETIC Cloud VPC %s did not become active within %s: %s",
					created.ID, createPollTimeout, pollErr.Error()),
			)
			return
		}
		// Re-fetch the authoritative record after polling — the initial
		// response may not reflect the final status / tags.
		fresh, err := r.client.GetVPC(ctx, created.ID)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to read VPC after provisioning",
				fmt.Sprintf("VPC %s reached active state but the follow-up GET failed: %s",
					created.ID, err.Error()),
			)
			return
		}
		final = fresh
	}

	if wantEgress {
		// An API predating #1734 ignores the field and creates a VPC without
		// dedicated egress: say so instead of failing on an inconsistent
		// result. The VPC stays in state (tainted).
		if !final.DedicatedEgressIP {
			resp.Diagnostics.Append(applyVPCToModel(ctx, final, &plan)...)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError(
				"Dedicated egress IP not supported by the platform",
				fmt.Sprintf("VPC %s was created but the CETIC Cloud API did not record "+
					"`dedicated_egress_ip = true`. The platform API is probably older than "+
					"this feature: upgrade it, or remove `dedicated_egress_ip`.", final.ID),
			)
			return
		}
		// A failure here is a warning, not an error: an error would taint the
		// VPC and the next apply would replace it (and everything inside it)
		// for an egress IP the platform keeps retrying on its own. The state
		// records the real `egress_ip_status`.
		last, waitErr := r.waitEgress(ctx, final.ID, true)
		if last != nil {
			final = last
		}
		if waitErr != nil {
			resp.Diagnostics.AddWarning(
				"Dedicated egress IP not in service",
				fmt.Sprintf("VPC %s was created, but its dedicated egress IP did not settle: %s",
					final.ID, waitErr.Error()),
			)
		}
	}

	diags = applyVPCToModel(ctx, final, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpcResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetVPC(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: VPC was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VPC",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we still surface verbatim in state —
	// the next plan/apply will pick up the eventual 404 and remove it.
	diags := applyVPCToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only handles `dedicated_egress_ip`: every other user-settable
// attribute carries RequiresReplace. The change is sent with PATCH, then the
// provider waits for the egress IP to settle: `attached` (or `allocated` when
// the VPC has no `snat = true` VNet yet) after enabling, no IP at all after
// disabling.
func (r *vpcResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vpcResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	want := plan.DedicatedEgressIP.ValueBool()
	if plan.DedicatedEgressIP.Equal(state.DedicatedEgressIP) {
		// Nothing the API can change in place: refresh and keep the state.
		got, err := r.client.GetVPC(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read VPC",
				fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()))
			return
		}
		resp.Diagnostics.Append(applyVPCToModel(ctx, got, &plan)...)
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	// 409 (VPC not active, no free IP, previous removal still running),
	// 422 (feature not offered, VPC not EVPN) and 429 (public IP quota) carry
	// a French detail meant for the user: surface it verbatim.
	patched, err := r.client.UpdateVPC(ctx, id, client.VPCUpdateRequest{DedicatedEgressIP: &want})
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update the dedicated egress IP of the VPC",
			fmt.Sprintf("CETIC Cloud rejected `dedicated_egress_ip = %t` for VPC %s: %s",
				want, id, err.Error()),
		)
		return
	}
	if patched.DedicatedEgressIP != want {
		resp.Diagnostics.AddError(
			"Dedicated egress IP not supported by the platform",
			fmt.Sprintf("The CETIC Cloud API did not record `dedicated_egress_ip = %t` for VPC %s. "+
				"The platform API is probably older than this feature.", want, id),
		)
		return
	}

	final := patched
	last, waitErr := r.waitEgress(ctx, id, want)
	if last != nil {
		final = last
	}
	// Record what the platform reports even on failure, so the state shows
	// the real `egress_ip_status` (e.g. `error`).
	resp.Diagnostics.Append(applyVPCToModel(ctx, final, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if waitErr != nil {
		action := "set up"
		if !want {
			action = "removed"
		}
		resp.Diagnostics.AddError(
			"Dedicated egress IP did not settle",
			fmt.Sprintf("The dedicated egress IP of VPC %s was not %s: %s", id, action, waitErr.Error()),
		)
	}
}

// waitEgress polls the VPC until its dedicated egress IP settles for the
// desired state `want` (see classifyEgressPoll). It returns the last VPC read,
// even on failure, so callers can record the real status.
func (r *vpcResource) waitEgress(ctx context.Context, id string, want bool) (*client.VPC, error) {
	var last *client.VPC
	err := client.Poll(ctx, egressPollInterval, egressPollTimeout, func(ctx context.Context) (bool, error) {
		cur, err := r.client.GetVPC(ctx, id)
		if err != nil {
			return false, err
		}
		last = cur
		return classifyEgressPoll(cur, want)
	})
	return last, err
}

// classifyEgressPoll tells whether the dedicated egress IP of `cur` has
// settled for the desired state `want`:
//   - want=true: done on `attached`, and on `allocated` (reserved, served as
//     soon as the VPC has a `snat = true` VNet: waiting for `attached` would
//     always time out on a VPC without one, e.g. right after its creation);
//   - want=false: done once no IP is linked any more (status null);
//   - `error` is terminal and fails in both directions.
//
// Any other status (`attaching`, `detaching`, or no IP yet) keeps polling.
func classifyEgressPoll(cur *client.VPC, want bool) (bool, error) {
	status := ""
	if cur.EgressIPStatus != nil {
		status = *cur.EgressIPStatus
	}
	if status == client.EgressIPStatusError {
		ip := "(unknown)"
		if cur.EgressIP != nil {
			ip = *cur.EgressIP
		}
		return false, fmt.Errorf("the dedicated egress IP %s of VPC %s is in `error` state; "+
			"the platform retries on its own — check the CETIC Cloud console, or contact "+
			"support if it persists", ip, cur.ID)
	}
	if want {
		return status == client.EgressIPStatusAttached || status == client.EgressIPStatusAllocated, nil
	}
	return status == "", nil
}

func (r *vpcResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpcResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Issue the DELETE, retrying on the transient 409 "contains VNets" that can
	// race with the children's still-settling teardown (see package doc). The
	// first attempt runs immediately; classifyVPCDeleteAttempt decides whether
	// each result is terminal, retryable, or fatal. lastConflict keeps the most
	// recent 409 detail so a budget-exhausting conflict surfaces actionably
	// rather than as a generic "polling timeout".
	var lastConflict error
	pollErr := client.Poll(ctx, deletePollInterval, deleteConflictRetryTimeout, func(ctx context.Context) (bool, error) {
		err := r.client.DeleteVPC(ctx, id)
		done, retry, fatal := classifyVPCDeleteAttempt(err)
		if fatal != nil {
			return false, fatal
		}
		if retry {
			lastConflict = err
		}
		return done, nil
	})
	if pollErr != nil {
		detail := pollErr
		if lastConflict != nil {
			detail = lastConflict
		}
		resp.Diagnostics.AddError(
			"Failed to delete VPC",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, detail.Error()),
		)
		return
	}

	// Poll until GetVPC returns 404. If the timeout elapses, warn but let
	// Terraform remove the resource from state — CETIC Cloud is still
	// converging asynchronously and blocking the apply would be worse.
	pollErr = client.Poll(ctx, deletePollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetVPC(ctx, id)
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
			"VPC deletion did not complete within the timeout",
			fmt.Sprintf("VPC %s was scheduled for deletion but did not disappear within %s: %s. "+
				"Terraform will remove the resource from state; the CETIC Cloud backend should "+
				"finish the teardown asynchronously.", id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// classifyVPCDeleteAttempt interprets the outcome of a single DELETE
// /v1/vpcs/{id} call during teardown:
//   - done=true  → the VPC is gone (2xx accepted, or 404 already-deleted);
//     stop and treat as success.
//   - retry=true → transient 409 (child VNet teardown still settling
//     server-side); keep retrying until the precondition clears.
//   - fatal!=nil → any other error; surface it to the user immediately.
//
// Extracted as a pure function so the retry decision is unit-tested without a
// live API (mirrors the publicip resource's classify*Poll helpers).
func classifyVPCDeleteAttempt(err error) (done bool, retry bool, fatal error) {
	switch {
	case err == nil:
		return true, false, nil
	case client.IsNotFound(err):
		return true, false, nil
	case client.IsConflict(err):
		return false, true, nil
	default:
		return false, false, err
	}
}

// ImportState lets users adopt an existing VPC with `terraform import
// ccp_vpc.example <uuid>`. Read fills the rest.
func (r *vpcResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyVPCToModel populates the framework model from the API representation.
// Always called after a successful Create/Read so state reflects the
// authoritative server view. Tags are normalised so a `nil` API response and
// an empty list both produce an empty list in state (avoids spurious diffs
// against an Optional+Computed list attribute).
func applyVPCToModel(ctx context.Context, src *client.VPC, dst *vpcResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.Name = types.StringValue(src.Name)
	dst.Region = types.StringValue(src.Region)
	dst.Status = types.StringValue(src.Status)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))

	// CIDR is null on legacy VPCs predating the field.
	if src.CIDR != nil {
		dst.Cidr = types.StringValue(*src.CIDR)
	} else {
		dst.Cidr = types.StringNull()
	}

	dst.DedicatedEgressIP = types.BoolValue(src.DedicatedEgressIP)
	dst.EgressIP = types.StringPointerValue(src.EgressIP)
	dst.EgressIPType = types.StringPointerValue(src.EgressIPType)
	dst.EgressIPStatus = types.StringPointerValue(src.EgressIPStatus)

	tagValues := make([]string, 0, len(src.Tags))
	tagValues = append(tagValues, src.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	dst.Tags = tagsList
	return diags
}

// tagsFromList converts the framework List representation into a Go slice.
// Null and unknown both collapse to nil so callers can hand the result
// straight to the API client.
func tagsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
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
