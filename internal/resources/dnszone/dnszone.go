// Package dnszone implements the ccp_dns_zone resource.
//
// A DNS zone is served ONLY inside the customer's own private network: the
// machines of that network get its name server automatically, and nothing is
// published to the internet.
//
// Three properties of the service drive the shape of this resource, and none
// of them is guessable from the schema alone:
//
//  1. `tier` belongs to the NETWORK, not to the zone. Every zone of the same
//     `vpc_id` shares one name server. Declaring a second zone in the same
//     network with a different `tier` is refused (409), and the message names
//     the level actually in place — it is relayed verbatim.
//  2. The name server is not a resource. It appears with the first zone of a
//     network and is torn down with the last one; there is nothing to create
//     or destroy on its own.
//  3. A PUBLIC domain name (`example.com`) is held in `pending_verification`
//     until its ownership record is published. An internal suffix
//     (`corp.internal`, `lan`, a single label…) has nothing to prove and goes
//     straight through. See `wait_for_verification`.
package dnszone

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*dnsZoneResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsZoneResource)(nil)
	_ resource.ResourceWithImportState = (*dnsZoneResource)(nil)
)

func New() resource.Resource { return &dnsZoneResource{} }

type dnsZoneResource struct{ client *client.Client }

// Provisioning a name server means standing up an appliance, so the wait is
// measured in minutes, not seconds. `verifyPoll` is longer still: it covers
// the propagation of a record the customer just published at their registrar.
const (
	provisionTimeout  = 15 * time.Minute
	verifyTimeout     = 20 * time.Minute
	pollInterval      = 10 * time.Second
	deleteWaitTimeout = 10 * time.Minute
)

type dnsZoneModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	VpcID         types.String `tfsdk:"vpc_id"`
	Tier          types.String `tfsdk:"tier"`
	DefaultTTL    types.Int64  `tfsdk:"default_ttl"`
	DNSSECEnabled types.Bool   `tfsdk:"dnssec_enabled"`

	WaitForVerification types.Bool `tfsdk:"wait_for_verification"`

	Status          types.String `tfsdk:"status"`
	Region          types.String `tfsdk:"region"`
	ErrorMessage    types.String `tfsdk:"error_message"`
	RecordSetsCount types.Int64  `tfsdk:"record_sets_count"`
	CreatedAt       types.String `tfsdk:"created_at"`

	ResolverAddresses      types.List   `tfsdk:"resolver_addresses"`
	ResolverEndpoints      types.List   `tfsdk:"resolver_endpoints"`
	ResolverTier           types.String `tfsdk:"resolver_tier"`
	ResolverStatus         types.String `tfsdk:"resolver_status"`
	NsHostname             types.String `tfsdk:"ns_hostname"`
	AppliesToNewGuestsOnly types.Bool   `tfsdk:"applies_to_new_guests_only"`

	OwnershipChallenge types.Object `tfsdk:"ownership_challenge"`
}

// endpointAttrTypes mirrors `resolver_endpoints` element attributes.
var endpointAttrTypes = map[string]attr.Type{
	"address":   types.StringType,
	"vnet_id":   types.StringType,
	"vnet_name": types.StringType,
	"vnet_cidr": types.StringType,
}

// challengeAttrTypes mirrors `ownership_challenge`.
var challengeAttrTypes = map[string]attr.Type{
	"record_name":  types.StringType,
	"record_type":  types.StringType,
	"record_value": types.StringType,
}

func (r *dnsZoneResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_dns_zone"
}

func (r *dnsZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a private DNS zone. The zone is answered **only inside the " +
			"private network you attach it to** — it is never published to the internet — and the " +
			"machines of that network receive its name server automatically.\n\n" +
			"~> **The service level is a property of the network, not of the zone.** All zones of the " +
			"same `vpc_id` are answered by the same name server. Declaring a second zone in that " +
			"network with a different `tier` is rejected; the error names the level already in place.\n\n" +
			"~> **Machines already running keep the name server they were given at creation.** " +
			"Turning private DNS on in a populated network does not make the zone visible from the " +
			"existing machines — recreate them, or create the zone before the machines.\n\n" +
			"Every argument forces replacement: a zone's name and the network it serves are its identity.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the zone.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Zone name, e.g. `corp.internal`. Internal suffixes " +
					"(`.internal`, `.lan`, `.home.arpa`, a single label such as `corp`…) are the main " +
					"use of the product and are accepted as-is. A **public** domain name " +
					"(`example.com`) additionally requires a proof of ownership — see " +
					"`wait_for_verification`. Normalised to lower case by the API. Forces replacement.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the private network (`ccp_vpc.id`) the zone is served " +
					"in. This is the **private network**, not one of its subnets: the name server " +
					"answers the same zones in every subnet of the network. A network with more than " +
					"nine subnets cannot be served, and zone creation is rejected rather than leaving " +
					"part of the machines without name resolution. Forces replacement.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"tier": schema.StringAttribute{
				MarkdownDescription: "Service level of the name server: `dev` (single server) or " +
					"`prod` (redundant name server with automatic failover). **Shared by every zone " +
					"of `vpc_id`** — asking for a level other than the one already serving that " +
					"network is rejected. Defaults to `dev`. Forces replacement.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("dev"),
				Validators: []validator.String{
					stringvalidator.OneOf("dev", "prod"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"default_ttl": schema.Int64Attribute{
				MarkdownDescription: "Default time-to-live of the zone, in seconds (60 to 604800). " +
					"Omit to take the platform default (3600 s) — the value is then read back from " +
					"the API. Forces replacement.",
				Optional: true,
				Computed: true,
				Validators: []validator.Int64{
					int64validator.Between(60, 604800),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"dnssec_enabled": schema.BoolAttribute{
				MarkdownDescription: "Signs the zone. On a private zone this protects against very " +
					"little — there is no chain of trust from the public root — so it defaults to " +
					"`false`. Forces replacement.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"wait_for_verification": schema.BoolAttribute{
				MarkdownDescription: "Only meaningful for a **public** domain name. Such a zone is " +
					"created on hold, and `ownership_challenge` names the `TXT` record to publish in " +
					"the public DNS of the domain before it can be served.\n\n" +
					"Leave at `false` (the default) on the apply that creates the zone: the apply " +
					"returns immediately with the record to publish. Once the record is live, set " +
					"this to `true` and apply again — that apply asks the platform to check the " +
					"proof and waits until the zone is answered.\n\n" +
					"Has no effect on an internal suffix, which has nothing to prove.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending_verification` (waiting for the ownership record), " +
					"`provisioning`, `active`, or `error`. `error` is terminal: delete the zone and " +
					"declare it again.",
				Computed: true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region the zone is served from — that of its private network.",
				Computed:            true,
			},
			"error_message": schema.StringAttribute{
				MarkdownDescription: "Why the zone could not be brought up, when `status` says so.",
				Computed:            true,
			},
			"record_sets_count": schema.Int64Attribute{
				MarkdownDescription: "Number of records **you** declared. The apex record the " +
					"platform maintains is not counted.",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"resolver_addresses": schema.ListAttribute{
				MarkdownDescription: "Addresses to use as name server from this network — **one per " +
					"subnet served**. Empty until the name server is up. From a machine, use the " +
					"address of ITS OWN subnet: they all answer the same zones, but each one is only " +
					"reachable from its own subnet. Use `resolver_endpoints` to know which is which.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"resolver_endpoints": schema.ListNestedAttribute{
				MarkdownDescription: "The same addresses, each with the subnet it serves. This is " +
					"the attribute to read when the network has more than one subnet — an address " +
					"taken from the wrong subnet does not answer.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"address":   schema.StringAttribute{Computed: true, MarkdownDescription: "Name server address to use from this subnet."},
						"vnet_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "UUID of the subnet."},
						"vnet_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the subnet. May be empty."},
						"vnet_cidr": schema.StringAttribute{Computed: true, MarkdownDescription: "CIDR of the subnet — tells two same-named subnets apart."},
					},
				},
			},
			"resolver_tier": schema.StringAttribute{
				MarkdownDescription: "Service level actually serving the network.\n\n" +
					"~> While the zone is still `pending_verification` this reports the level you " +
					"**asked for**, not one that is running: no name server exists yet.",
				Computed: true,
			},
			"resolver_status": schema.StringAttribute{
				MarkdownDescription: "State of the name server itself — `provisioning`, `active` or " +
					"`error`. Observed directly, never derived from the state of the zone.",
				Computed: true,
			},
			"ns_hostname": schema.StringAttribute{
				MarkdownDescription: "Name of the server published at the apex of the zone. " +
					"Informational: it only resolves through that name server.",
				Computed: true,
			},
			"applies_to_new_guests_only": schema.BoolAttribute{
				MarkdownDescription: "Always `true`: machines receive the name server when they are " +
					"created. Existing machines keep the one they already have.",
				Computed: true,
			},
			"ownership_challenge": schema.SingleNestedAttribute{
				MarkdownDescription: "The record to publish in the **public** DNS of the domain to " +
					"prove you own it. Null on an internal suffix, and null again once the proof has " +
					"been accepted.",
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"record_name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the record to publish."},
					"record_type":  schema.StringAttribute{Computed: true, MarkdownDescription: "Always `TXT`."},
					"record_value": schema.StringAttribute{Computed: true, MarkdownDescription: "Exact value to publish."},
				},
			},
		},
	}
}

func (r *dnsZoneResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// stateFrom maps the API zone onto the model. `wantWait` is carried over from
// the plan/state because `wait_for_verification` is a provider-side switch the
// API knows nothing about — mapping it from a response would blank it.
func stateFrom(ctx context.Context, z *client.DNSZone, wantWait types.Bool) (dnsZoneModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := dnsZoneModel{
		ID:                  types.StringValue(z.ID),
		Name:                types.StringValue(z.Name),
		VpcID:               types.StringValue(z.VpcID),
		DefaultTTL:          types.Int64Value(z.DefaultTTL),
		DNSSECEnabled:       types.BoolValue(z.DNSSECEnabled),
		WaitForVerification: wantWait,
		Status:              types.StringValue(z.Status),
		Region:              types.StringValue(z.Region),
		CreatedAt:           types.StringValue(z.CreatedAt),
	}
	if wantWait.IsNull() || wantWait.IsUnknown() {
		m.WaitForVerification = types.BoolValue(false)
	}

	if z.ErrorMessage != nil {
		m.ErrorMessage = types.StringValue(*z.ErrorMessage)
	} else {
		m.ErrorMessage = types.StringNull()
	}
	if z.RecordSetsCount != nil {
		m.RecordSetsCount = types.Int64Value(*z.RecordSetsCount)
	} else {
		m.RecordSetsCount = types.Int64Null()
	}

	// `tier` is what the caller asked for; the API reports the level actually
	// serving the network under `resolver.tier`. Keeping them in separate
	// attributes is what makes the difference visible instead of silently
	// rewriting the configured value (CLAUDE.md pitfall #1).
	endpointsType := types.ObjectType{AttrTypes: endpointAttrTypes}
	if z.Resolver == nil {
		// List shape: the resolver block is simply absent. Preserving the
		// configured/known values rather than writing zeroes is pitfall #5.
		m.ResolverAddresses = types.ListNull(types.StringType)
		m.ResolverEndpoints = types.ListNull(endpointsType)
		m.ResolverTier = types.StringNull()
		m.ResolverStatus = types.StringNull()
		m.NsHostname = types.StringNull()
		m.AppliesToNewGuestsOnly = types.BoolNull()
		m.Tier = types.StringNull()
	} else {
		addrs, d := types.ListValueFrom(ctx, types.StringType, z.Resolver.Addresses)
		diags.Append(d...)
		m.ResolverAddresses = addrs

		elems := make([]attr.Value, 0, len(z.Resolver.Endpoints))
		for _, e := range z.Resolver.Endpoints {
			cidr := types.StringNull()
			if e.VnetCIDR != nil {
				cidr = types.StringValue(*e.VnetCIDR)
			}
			obj, d := types.ObjectValue(endpointAttrTypes, map[string]attr.Value{
				"address":   types.StringValue(e.Address),
				"vnet_id":   types.StringValue(e.VnetID),
				"vnet_name": types.StringValue(e.VnetName),
				"vnet_cidr": cidr,
			})
			diags.Append(d...)
			elems = append(elems, obj)
		}
		list, d := types.ListValue(endpointsType, elems)
		diags.Append(d...)
		m.ResolverEndpoints = list

		m.ResolverTier = types.StringValue(z.Resolver.Tier)
		m.ResolverStatus = types.StringValue(z.Resolver.Status)
		m.NsHostname = types.StringValue(z.Resolver.NsHostname)
		m.AppliesToNewGuestsOnly = types.BoolValue(z.Resolver.AppliesToNewGuestsOnly)
		m.Tier = types.StringValue(z.Resolver.Tier)
	}

	if z.OwnershipChallenge == nil {
		m.OwnershipChallenge = types.ObjectNull(challengeAttrTypes)
	} else {
		obj, d := types.ObjectValue(challengeAttrTypes, map[string]attr.Value{
			"record_name":  types.StringValue(z.OwnershipChallenge.RecordName),
			"record_type":  types.StringValue(z.OwnershipChallenge.RecordType),
			"record_value": types.StringValue(z.OwnershipChallenge.RecordValue),
		})
		diags.Append(d...)
		m.OwnershipChallenge = obj
	}
	return m, diags
}

// applyTier keeps the configured `tier` in state.
//
// `tier` is Required-like intent: the API answers with `resolver.tier`, the
// level actually serving the network, which can differ from the request while
// the zone waits for its ownership proof. Writing the API value over the
// configured one would produce "inconsistent result after apply" — so the
// configured value wins in `tier`, and the served one stays in `resolver_tier`.
func applyTier(m *dnsZoneModel, configured types.String) {
	if !configured.IsNull() && !configured.IsUnknown() {
		m.Tier = configured
	} else if m.Tier.IsNull() {
		m.Tier = types.StringValue("dev")
	}
}

func (r *dnsZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cr := client.DNSZoneCreateRequest{
		Name:          plan.Name.ValueString(),
		VpcID:         plan.VpcID.ValueString(),
		DNSSECEnabled: plan.DNSSECEnabled.ValueBool(),
	}
	if !plan.Tier.IsNull() && !plan.Tier.IsUnknown() {
		cr.Tier = plan.Tier.ValueString()
	}
	if !plan.DefaultTTL.IsNull() && !plan.DefaultTTL.IsUnknown() {
		v := plan.DefaultTTL.ValueInt64()
		cr.DefaultTTL = &v
	}

	created, err := r.client.CreateDNSZone(ctx, cr)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create DNS zone", err.Error())
		return
	}

	// ⚠️ State is written even when the zone does not come up. From here on the
	// zone EXISTS server-side, and its name is taken: returning on the error
	// without recording it would leave a zone nothing references, which the next
	// apply could not create (409) nor destroy.
	final := r.settle(ctx, created, plan.WaitForVerification.ValueBool(), &resp.Diagnostics)
	state, diags := stateFrom(ctx, final, plan.WaitForVerification)
	resp.Diagnostics.Append(diags...)
	applyTier(&state, plan.Tier)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// settle drives the zone to a usable state, and says out loud what is left to
// do when it cannot. It never fails the apply on `pending_verification`: the
// zone exists, the record to publish is in state, and failing would destroy
// that information on the next apply.
func (r *dnsZoneResource) settle(ctx context.Context, z *client.DNSZone, wait bool, diags *diag.Diagnostics) *client.DNSZone {
	if z.Status == client.DNSZoneStatusPendingVerification {
		if !wait {
			diags.AddWarning(
				"DNS zone waiting for proof of ownership",
				fmt.Sprintf(
					"The zone %q is a public domain name, so it is not served yet. Publish the "+
						"record given in `ownership_challenge` in the public DNS of the domain, then "+
						"set `wait_for_verification = true` and apply again.", z.Name,
				),
			)
			return z
		}
		verified, err := r.pollVerify(ctx, z.ID)
		if err != nil {
			diags.AddError("Proof of ownership not accepted", err.Error())
			return z
		}
		z = verified
	}

	if z.Status == client.DNSZoneStatusActive {
		return z
	}

	final, err := r.pollUntilServed(ctx, z.ID)
	if err != nil {
		diags.AddError("DNS zone did not come up", err.Error())
		return z
	}
	return final
}

// pollVerify replays POST /verify until the platform accepts the proof. The
// call is idempotent, and a rejection is not an error state — the record is
// simply not visible yet from the platform's resolvers.
func (r *dnsZoneResource) pollVerify(ctx context.Context, id string) (*client.DNSZone, error) {
	deadline := time.Now().Add(verifyTimeout)
	var lastErr error
	for {
		z, err := r.client.VerifyDNSZone(ctx, id)
		if err == nil && z.Status != client.DNSZoneStatusPendingVerification {
			return z, nil
		}
		if err != nil {
			if client.IsNotFound(err) {
				return nil, err
			}
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return nil, fmt.Errorf(
					"still waiting for the ownership record after %s — last answer: %w",
					verifyTimeout, lastErr,
				)
			}
			return nil, fmt.Errorf(
				"still waiting for the ownership record after %s", verifyTimeout,
			)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
}

func (r *dnsZoneResource) pollUntilServed(ctx context.Context, id string) (*client.DNSZone, error) {
	deadline := time.Now().Add(provisionTimeout)
	for {
		z, err := r.client.GetDNSZone(ctx, id)
		if err != nil {
			return nil, err
		}
		switch z.Status {
		case client.DNSZoneStatusActive:
			return z, nil
		case client.DNSZoneStatusError:
			msg := "no reason given"
			if z.ErrorMessage != nil {
				msg = *z.ErrorMessage
			}
			return z, fmt.Errorf(
				"the zone is in a terminal error state (%s). Delete it and declare it again", msg,
			)
		}
		if time.Now().After(deadline) {
			return z, fmt.Errorf("timed out after %s (last state: %s)", provisionTimeout, z.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (r *dnsZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.GetDNSZone(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}
	next, diags := stateFrom(ctx, z, state.WaitForVerification)
	resp.Diagnostics.Append(diags...)
	applyTier(&next, state.Tier)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

// Update handles the only non-replacing attribute of this resource:
// `wait_for_verification`. Flipping it to `true` is the deliberate second
// apply that checks the proof of ownership and waits for the zone to be
// served — everything else forces replacement.
func (r *dnsZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	z, err := r.client.GetDNSZone(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}

	final := r.settle(ctx, z, plan.WaitForVerification.ValueBool(), &resp.Diagnostics)
	next, diags := stateFrom(ctx, final, plan.WaitForVerification)
	resp.Diagnostics.Append(diags...)
	applyTier(&next, plan.Tier)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *dnsZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.client.DeleteDNSZone(ctx, id); err != nil {
		if client.IsNotFound(err) {
			return
		}
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"DNS zone still carries records",
				err.Error()+"\n\nDeleting a zone is not a cascade. Remove its `ccp_dns_record` "+
					"resources first — Terraform does this on its own when they are declared in the "+
					"same configuration.",
			)
			return
		}
		resp.Diagnostics.AddError("Failed to delete DNS zone", err.Error())
		return
	}
	// Tearing the zone down is asynchronous, and the name server of the
	// network goes with the last zone. Returning early would let a replace
	// re-enter Create while the name is still taken (409).
	if err := client.PollUntilDeleted(ctx, deleteWaitTimeout, func(ctx context.Context) error {
		_, err := r.client.GetDNSZone(ctx, id)
		return err
	}); err != nil {
		resp.Diagnostics.AddError("DNS zone deletion did not complete", err.Error())
	}
}

// ImportState takes the zone UUID.
//
// ⚠️ `tier` and `dnssec_enabled` carry a default AND force replacement, so a
// configuration that omits them plans `dev` and `false`. On a zone served at
// `prod`, the first plan after an import would propose to destroy it. The
// warning below is the only place the operator sees that before it happens.
func (r *dnsZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	// Not a server-side attribute: a freshly imported zone has nothing pending
	// from Terraform's point of view.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("wait_for_verification"), false)...)
	resp.Diagnostics.AddWarning(
		"Check `tier` and `dnssec_enabled` before applying",
		"Both carry a default (`dev`, `false`) and both force replacement. If this zone uses "+
			"anything else, write the real values into the configuration before the next plan — "+
			"otherwise it will propose to destroy and recreate the zone. `resolver_tier` and "+
			"`dnssec_enabled` in the imported state give you the values to write.",
	)
}
