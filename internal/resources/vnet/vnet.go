// Package vnet implements the ccp_vnet Terraform resource.
//
// A VNet in CETIC Cloud is a Proxmox SDN VXLAN VNet nested under a VPC, with
// IPAM allocations served by the per-VPC NAT GW LXC. Most fields are
// immutable post-create (CIDR, DHCP range, parent VPC) and force replacement
// on change. Only `name` and `snat` are mutable via the PATCH endpoint.
//
// Provisioning is asynchronous: POST /v1/vpcs/{vpc_id}/vnets returns 201
// immediately while the NAT GW NIC attach + iptables MASQUERADE sync runs in
// the Celery worker. We poll GetVNet (list-and-filter under the parent VPC)
// every 5 s up to 90 s until the VNet reaches `active` (or `error`).
//
// Deletion is asynchronous as well — the VNet enters `deleting` until the NAT
// GW NIC detach + IPAM cleanup completes, then disappears from the list. We
// poll for 404 up to 60 s; if the timeout elapses we surface a warning rather
// than a hard error so Terraform still removes the resource from state.
//
// The 409 Conflict response on delete (containers/VMs/LBs still attached)
// passes through as a hard error so the user keeps the resource in state and
// can clean up dependents before retrying.
package vnet

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*vnetResource)(nil)
	_ resource.ResourceWithConfigure   = (*vnetResource)(nil)
	_ resource.ResourceWithImportState = (*vnetResource)(nil)
)

// New returns a freshly-constructed ccp_vnet resource. Wired in by
// provider.go via vnet.New.
func New() resource.Resource {
	return &vnetResource{}
}

// vnetResource is the framework Resource implementation. The client is stashed
// in Configure and reused by Create/Read/Update/Delete.
type vnetResource struct {
	client *client.Client
}

// vnetResourceModel mirrors the schema below 1-to-1. Tag names must match the
// schema attribute keys exactly.
type vnetResourceModel struct {
	ID        types.String `tfsdk:"id"`
	VPCID     types.String `tfsdk:"vpc_id"`
	Name      types.String `tfsdk:"name"`
	CIDR      types.String `tfsdk:"cidr"`
	DHCPStart types.String `tfsdk:"dhcp_start"`
	DHCPEnd   types.String `tfsdk:"dhcp_end"`
	SNAT      types.Bool   `tfsdk:"snat"`
	Tags      types.List   `tfsdk:"tags"`
	Gateway   types.String `tfsdk:"gateway"`
	Isolated  types.Bool   `tfsdk:"isolated"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`
}

// Validation patterns.
//
// nameValidatorPattern matches alphanumerics + `_`/`-` like the API.
//
// uuidPattern is loose enough to accept v1/v4/v7 UUIDs without forcing a
// specific version. Used to fail fast on obviously-wrong vpc_id values
// before we even hit the API.
//
// cidrPattern accepts a dotted-quad followed by /16..28 — the API rejects
// anything outside that range so we mirror the constraint client-side for
// quicker feedback. Octet ranges are NOT enforced here (the API does that).
//
// ipv4Pattern is a loose dotted-quad check used for dhcp_start/dhcp_end —
// the authoritative range validation (must lie inside the CIDR, start <= end)
// is performed by the API.
var (
	nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	uuidPattern          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	cidrPattern          = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}/(1[6-9]|2[0-8])$`)
	ipv4Pattern          = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}$`)
)

// Polling parameters — Create waits up to 90 s for the VNet to leave the
// transitional state, Delete waits up to 60 s for the resource to disappear.
const (
	createPollInterval = 5 * time.Second
	createPollTimeout  = 90 * time.Second
	deletePollInterval = 5 * time.Second
	deletePollTimeout  = 60 * time.Second
)

func (r *vnetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vnet"
}

func (r *vnetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VNet inside a VPC. A VNet is a Proxmox SDN " +
			"VXLAN VNet with its own CIDR and (optional) DHCP range, served by the per-VPC " +
			"NAT gateway. Only `name` and `snat` can be updated in place; changes to `vpc_id`, " +
			"`cidr`, `dhcp_start`, `dhcp_end`, or `tags` force replacement. Creation is " +
			"asynchronous: the provider polls until the VNet reaches `active` (up to 90 seconds).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the VNet.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent VPC. VNets cannot move between VPCs, " +
					"so any change forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						uuidPattern,
						"must be a UUID (e.g. as returned by `ccp_vpc.id`)",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "VNet name (max 100 chars; alphanumerics, `_`, and `-`). " +
					"Mutable in place via PATCH.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(100),
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must contain only letters, digits, underscores, or hyphens",
					),
				},
			},
			"cidr": schema.StringAttribute{
				MarkdownDescription: "Private IPv4 CIDR for the VNet. Must be a `/16` to `/28` " +
					"block (e.g. `10.0.0.0/24`). Cannot be changed after creation — any change " +
					"forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						cidrPattern,
						"must be a dotted-quad CIDR with prefix length between /16 and /28 (e.g. 10.0.0.0/24)",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"dhcp_start": schema.StringAttribute{
				MarkdownDescription: "First IPv4 address of the DHCP range (must lie inside `cidr`). " +
					"Optional; if omitted the API picks defaults. Must be paired with `dhcp_end`. " +
					"Immutable — change forces replacement.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						ipv4Pattern,
						"must be a dotted-quad IPv4 address (e.g. 10.0.0.51)",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dhcp_end": schema.StringAttribute{
				MarkdownDescription: "Last IPv4 address of the DHCP range (must lie inside `cidr` " +
					"and be >= `dhcp_start`). Optional; pair with `dhcp_start`. Immutable — change " +
					"forces replacement.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						ipv4Pattern,
						"must be a dotted-quad IPv4 address (e.g. 10.0.0.254)",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"snat": schema.BoolAttribute{
				MarkdownDescription: "Whether outbound traffic from this VNet is SNAT'd through " +
					"the per-VPC NAT gateway. Defaults to `false`. Mutable in place via PATCH; " +
					"setting to `false` is rejected (409) if IPaaS routed public IPs are still " +
					"attached to instances in this VNet.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the VNet. The PATCH endpoint " +
					"does not accept tag changes, so any modification forces replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"gateway": schema.StringAttribute{
				MarkdownDescription: "Default gateway IPv4 of the VNet (the NAT GW LXC interface " +
					"on this VNet, conventionally `<cidr>.1`). Computed by the API.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"isolated": schema.BoolAttribute{
				MarkdownDescription: "Whether VNet isolation is enabled. When `true`, the global firewall switch on this VNet is on — DROP applies by default to inter-VNet traffic and ACCEPT rules from `ccp_vnet_firewall_rule` are required to allow specific flows. The provider toggles this via the dedicated `PUT /v1/vnets/{id}/firewall/isolation` endpoint after the VNet is created (mutable in place).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
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
				MarkdownDescription: "RFC 3339 timestamp at which the VNet was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *vnetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *vnetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vnetResourceModel
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

	createReq := client.VNetCreateRequest{
		Name:      plan.Name.ValueString(),
		CIDR:      plan.CIDR.ValueString(),
		DHCPStart: stringPtrOrNil(plan.DHCPStart),
		DHCPEnd:   stringPtrOrNil(plan.DHCPEnd),
		SNAT:      plan.SNAT.ValueBool(),
		Tags:      tags,
	}

	vpcID := plan.VPCID.ValueString()
	created, err := r.client.CreateVNet(ctx, vpcID, createReq)
	if err != nil {
		// 409 Conflict (CIDR overlap, name collision, DHCP outside CIDR) and
		// other client errors carry a French detail message we want to
		// surface verbatim — most of these are user actionable.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VNet creation conflicts with an existing resource",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create VNet",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
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
			"VNet entered error state during provisioning",
			fmt.Sprintf("VNet %s reported status `error` immediately after creation. "+
				"Check the CETIC Cloud console or backoffice for the underlying cause.", created.ID),
		)
		return
	default:
		pollErr := client.Poll(ctx, createPollInterval, createPollTimeout, func(ctx context.Context) (bool, error) {
			cur, err := r.client.GetVNet(ctx, vpcID, created.ID)
			if err != nil {
				return false, err
			}
			switch cur.Status {
			case client.StatusError:
				return false, fmt.Errorf("VNet %s entered error state during provisioning", cur.ID)
			case client.StatusActive:
				return true, nil
			default:
				return false, nil
			}
		})
		if pollErr != nil {
			resp.Diagnostics.AddError(
				"VNet failed to reach active state",
				fmt.Sprintf("CETIC Cloud VNet %s did not become active within %s: %s",
					created.ID, createPollTimeout, pollErr.Error()),
			)
			return
		}
		// Re-fetch the authoritative record after polling — the initial
		// response may not reflect the final gateway / dhcp_* / tags.
		fresh, err := r.client.GetVNet(ctx, vpcID, created.ID)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to read VNet after provisioning",
				fmt.Sprintf("VNet %s reached active state but the follow-up GET failed: %s",
					created.ID, err.Error()),
			)
			return
		}
		final = fresh
	}

	// Capture the user's isolation intent BEFORE applyVNetToModel runs, since
	// it overwrites plan.Isolated with the backend value (always false right
	// after create — the dedicated /firewall/isolation endpoint hasn't been
	// called yet).
	wantIsolated := !plan.Isolated.IsNull() && !plan.Isolated.IsUnknown() && plan.Isolated.ValueBool()

	diags = applyVNetToModel(ctx, final, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If the user requested isolation=true at create-time, toggle it via the
	// dedicated firewall endpoint (the base POST /vnets does not accept it).
	if wantIsolated && !final.Isolated {
		if err := r.client.SetVNetIsolation(ctx, final.ID, true); err != nil {
			resp.Diagnostics.AddError(
				"Failed to enable VNet isolation",
				fmt.Sprintf("VNet %s was created but the isolation toggle failed: %s", final.ID, err.Error()),
			)
			return
		}
		plan.Isolated = types.BoolValue(true)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vnetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vnetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetVNet(ctx, state.VPCID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: VNet (or its parent VPC) was deleted
			// out-of-band. The list endpoint 404s if the VPC is gone, which
			// IsNotFound also catches — either way the right move is to drop
			// this resource from state.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VNet",
			fmt.Sprintf("CETIC Cloud API error for vpc=%s vnet=%s: %s",
				state.VPCID.ValueString(), state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we still surface verbatim in state —
	// the next plan/apply will pick up the eventual 404 and remove it.
	diags := applyVNetToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update handles in-place changes to `name` and `snat` only. Every other
// attribute carries RequiresReplace, so the framework triggers destroy+create
// for them and Update is never called with those changes.
//
// We diff plan vs prior state and only send fields that actually changed —
// PATCH with no fields would still be valid but pointless, and sending the
// old value of `snat` could spuriously trigger the IPaaS-attachment 409 check.
func (r *vnetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vnetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := client.VNetUpdateRequest{}
	hasChange := false

	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		updateReq.Name = &v
		hasChange = true
	}
	if !plan.SNAT.Equal(state.SNAT) {
		v := plan.SNAT.ValueBool()
		updateReq.SNAT = &v
		hasChange = true
	}

	vpcID := state.VPCID.ValueString()
	id := state.ID.ValueString()

	// Toggle isolation via the dedicated endpoint when it changes.
	if !plan.Isolated.Equal(state.Isolated) && !plan.Isolated.IsNull() && !plan.Isolated.IsUnknown() {
		if err := r.client.SetVNetIsolation(ctx, id, plan.Isolated.ValueBool()); err != nil {
			resp.Diagnostics.AddError(
				"Failed to toggle VNet isolation",
				fmt.Sprintf("CETIC Cloud API error for vnet=%s: %s", id, err.Error()),
			)
			return
		}
	}

	if hasChange {
		if _, err := r.client.UpdateVNet(ctx, vpcID, id, updateReq); err != nil {
			// 409 most commonly means "snat=false rejected because IPaaS
			// routed IPs are still attached" — surface the API detail so the
			// user knows what to detach.
			if client.IsConflict(err) {
				resp.Diagnostics.AddError(
					"VNet update conflicts with current state",
					fmt.Sprintf("CETIC Cloud rejected the update for vnet %s: %s. "+
						"For `snat=false`, ensure no IPaaS routed public IPs remain attached "+
						"to instances in this VNet before retrying.", id, err.Error()),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Failed to update VNet",
				fmt.Sprintf("CETIC Cloud API error for vpc=%s vnet=%s: %s", vpcID, id, err.Error()),
			)
			return
		}
	}

	// Always re-fetch so computed fields and any server-side normalisation
	// are reflected in state. Cheap (one GET) and avoids stale state if the
	// PATCH response shape differs from the GET shape.
	fresh, err := r.client.GetVNet(ctx, vpcID, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read VNet after update",
			fmt.Sprintf("CETIC Cloud API error for vpc=%s vnet=%s: %s", vpcID, id, err.Error()),
		)
		return
	}

	diags := applyVNetToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vnetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vnetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := state.VPCID.ValueString()
	id := state.ID.ValueString()
	if err := r.client.DeleteVNet(ctx, vpcID, id); err != nil {
		// Treat "already gone" as success — no point erroring on destroy when
		// the desired end state is already reached.
		if client.IsNotFound(err) {
			return
		}
		// 409 — containers/VMs/LBs still attached. Surface the detail so the
		// user can clean up; we deliberately return an error so Terraform
		// keeps the resource in state instead of orphaning the dependents.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VNet delete blocked by attached resources",
				fmt.Sprintf("CETIC Cloud refused to delete vnet %s: %s. "+
					"Detach any containers, VMs, or load balancers using this VNet, then retry.",
					id, err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete VNet",
			fmt.Sprintf("CETIC Cloud API error for vpc=%s vnet=%s: %s", vpcID, id, err.Error()),
		)
		return
	}

	// Poll until GetVNet returns 404. If the timeout elapses, warn but let
	// Terraform remove the resource from state — CETIC Cloud is still
	// converging asynchronously and blocking the apply would be worse.
	pollErr := client.Poll(ctx, deletePollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetVNet(ctx, vpcID, id)
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
			"VNet deletion did not complete within the timeout",
			fmt.Sprintf("VNet %s was scheduled for deletion but did not disappear within %s: %s. "+
				"Terraform will remove the resource from state; the CETIC Cloud backend should "+
				"finish the teardown asynchronously.", id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing VNet with
// `terraform import ccp_vnet.example <vpc_id>:<vnet_id>`.
//
// We need both the parent VPC ID and the VNet ID because the API has no
// flat GET endpoint — every read goes through /v1/vpcs/{vpc_id}/vnets.
// Read fills the rest of the state from the API.
func (r *vnetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected import ID in the form `<vpc_id>:<vnet_id>`, got %q. "+
				"Both UUIDs are required because the API exposes VNets only under their parent VPC.",
				req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vpc_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

// applyVNetToModel populates the framework model from the API representation.
// Always called after a successful Create/Read/Update so state reflects the
// authoritative server view.
//
// Pointer fields on the API struct (Gateway, DHCPStart, DHCPEnd) collapse to
// null framework values when nil — the API leaves them unset for VNets that
// haven't been provisioned far enough yet, or when the client didn't request
// a DHCP range explicitly.
//
// Tags are normalised so a `nil` API response and an empty list both produce
// an empty list in state — avoids spurious diffs against an Optional+Computed
// list attribute.
func applyVNetToModel(ctx context.Context, src *client.VNet, dst *vnetResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.VPCID = types.StringValue(src.VPCID)
	dst.Name = types.StringValue(src.Name)
	dst.CIDR = types.StringValue(src.CIDR)
	dst.SNAT = types.BoolValue(src.SNAT)
	dst.Isolated = types.BoolValue(src.Isolated)
	dst.Status = types.StringValue(src.Status)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))

	if src.Gateway != nil {
		dst.Gateway = types.StringValue(*src.Gateway)
	} else {
		dst.Gateway = types.StringNull()
	}
	if src.DHCPStart != nil {
		dst.DHCPStart = types.StringValue(*src.DHCPStart)
	} else {
		dst.DHCPStart = types.StringNull()
	}
	if src.DHCPEnd != nil {
		dst.DHCPEnd = types.StringValue(*src.DHCPEnd)
	} else {
		dst.DHCPEnd = types.StringNull()
	}

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

// stringPtrOrNil maps a framework String to *string, returning nil for null
// or unknown values. Used to populate Optional pointer fields on the API
// request struct (DHCPStart/DHCPEnd) without sending empty strings.
func stringPtrOrNil(s types.String) *string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	v := s.ValueString()
	return &v
}
