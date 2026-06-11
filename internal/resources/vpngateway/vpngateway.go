// Package vpngateway implements the ccp_vpn_gateway Terraform resource.
//
// A VPN gateway is a managed WireGuard appliance that fronts the private
// networks of one or more VPCs: remote clients (modelled by ccp_vpn_peer)
// reach otherwise-unreachable private hosts through an encrypted tunnel instead
// of exposing instances to the public internet.
//
// CRUD semantics (mirrors ccp_bastion):
//   - Create : POST /v1/vpn/gateways — returns the appliance metadata. The
//     WireGuard endpoint (`endpoint_host`/`endpoint_port`/`public_key`) and the
//     attached public IP are populated once provisioning finishes, so they are
//     Computed.
//   - Read   : GET /v1/vpn/gateways/{id}. 404 ⇒ removed from state (drift).
//   - Delete : DELETE /v1/vpn/gateways/{id}, then poll until the appliance is
//     really gone — teardown is asynchronous, and without the wait a replace
//     (destroy-then-create with the same name) would race the still-present
//     appliance and get a 409.
//
// The CETIC Cloud API has no update endpoint for gateway core fields, so every
// settable attribute forces replacement and Update is a guarded no-op.
package vpngateway

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*vpnGatewayResource)(nil)
	_ resource.ResourceWithConfigure   = (*vpnGatewayResource)(nil)
	_ resource.ResourceWithImportState = (*vpnGatewayResource)(nil)
)

// New returns a freshly-constructed ccp_vpn_gateway resource. Wired in by
// provider.go via vpngateway.New.
func New() resource.Resource {
	return &vpnGatewayResource{}
}

type vpnGatewayResource struct {
	client *client.Client
}

// vpnGatewayResourceModel mirrors the schema below 1-to-1. Tag names must match
// the schema attribute keys exactly.
type vpnGatewayResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	Plan            types.String `tfsdk:"plan"`
	VpcIDs          types.List   `tfsdk:"vpc_ids"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	PeerPoolCIDR    types.String `tfsdk:"peer_pool_cidr"`
	DNS             types.String `tfsdk:"dns"`
	Tags            types.List   `tfsdk:"tags"`
	Status          types.String `tfsdk:"status"`
	EndpointHost    types.String `tfsdk:"endpoint_host"`
	EndpointPort    types.Int64  `tfsdk:"endpoint_port"`
	PublicKey       types.String `tfsdk:"public_key"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscore,
// hyphen, and space, max 100 chars (length enforced separately).
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\- ]+$`)

func (r *vpnGatewayResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vpn_gateway"
}

func (r *vpnGatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VPN gateway — a managed WireGuard appliance that fronts " +
			"the private networks of one or more VPCs. Remote clients (modelled by `ccp_vpn_peer`) reach " +
			"otherwise-unreachable private hosts through an encrypted tunnel instead of exposing instances " +
			"to the public internet. The CETIC Cloud API has no update endpoint for the gateway's core " +
			"fields, so any change forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the VPN gateway.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (max 100 chars; alphanumerics, `_`, `-`, and spaces). " +
					"Immutable — changing forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(100),
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must contain only letters, digits, underscores, hyphens, or spaces",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code the gateway is provisioned in (e.g. `RNN`). " +
					"Immutable — changing forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Sizing plan: `small`, `medium`, or `large`. " +
					"Immutable — changing forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("small", "medium", "large"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vpc_ids": schema.ListAttribute{
				MarkdownDescription: "UUIDs of the VPCs whose private networks the gateway tunnels into. " +
					"The first entry is the primary VPC. Immutable — changing forces replacement.",
				Required:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a reserved public IP to attach to the gateway endpoint. " +
					"If omitted, the platform allocates one (IPaaS). Immutable — changing forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"peer_pool_cidr": schema.StringAttribute{
				MarkdownDescription: "CIDR block the gateway allocates peer tunnel IPs from. " +
					"If omitted, the platform picks one. Immutable — changing forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dns": schema.StringAttribute{
				MarkdownDescription: "DNS server pushed to peers in their generated WireGuard config. " +
					"Immutable — changing forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the gateway. The API has no endpoint to " +
					"mutate tags after creation, so changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"endpoint_host": schema.StringAttribute{
				MarkdownDescription: "Public WireGuard endpoint hostname (or IP) clients connect to. " +
					"Populated once the appliance finishes provisioning.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"endpoint_port": schema.Int64Attribute{
				MarkdownDescription: "UDP port of the WireGuard endpoint. Populated once the appliance " +
					"finishes provisioning.",
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard public key of the gateway, needed in each peer's config. " +
					"Populated once the appliance finishes provisioning.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IP address attached to the gateway endpoint. " +
					"Populated once the appliance finishes provisioning.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Lifecycle status: `provisioning`, `active`, `error`, or `deleting`. " +
					"Read-only and volatile.",
				Computed: true,
			},
		},
	}
}

func (r *vpnGatewayResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
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

// applyToModel maps an API VPNGateway onto the Terraform model. `status` is
// volatile (known-after-apply) so it carries no UseStateForUnknown — see
// CLAUDE.md (v2.0.4 fix). Optional+Computed fields that the API may omit from
// its response are preserved on the model rather than blindly overwritten with
// a zero value (CLAUDE.md pitfall #5).
func applyToModel(ctx context.Context, m *vpnGatewayResourceModel, g *client.VPNGateway) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(g.ID)
	m.Name = types.StringValue(g.Name)
	m.Region = types.StringValue(g.Region)
	m.Plan = types.StringValue(g.Plan)
	m.Status = types.StringValue(g.Status)

	// vpc_ids: prefer the list; fall back to the single vpc_id for older
	// responses. Preserve the existing value if the API returns neither.
	ids := g.VpcIDs
	if len(ids) == 0 && g.VpcID != "" {
		ids = []string{g.VpcID}
	}
	if len(ids) > 0 || m.VpcIDs.IsNull() || m.VpcIDs.IsUnknown() {
		vpcList, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		if diags.HasError() {
			return diags
		}
		m.VpcIDs = vpcList
	}

	if g.PublicIPID != nil {
		m.PublicIPID = types.StringValue(*g.PublicIPID)
	} else if m.PublicIPID.IsUnknown() {
		m.PublicIPID = types.StringNull()
	}
	if g.PeerPoolCIDR != nil {
		m.PeerPoolCIDR = types.StringValue(*g.PeerPoolCIDR)
	} else if m.PeerPoolCIDR.IsUnknown() {
		m.PeerPoolCIDR = types.StringNull()
	}
	if g.DNS != nil {
		m.DNS = types.StringValue(*g.DNS)
	} else if m.DNS.IsUnknown() {
		m.DNS = types.StringNull()
	}

	tagValues := make([]string, 0, len(g.Tags))
	tagValues = append(tagValues, g.Tags...)
	tagsList, d := types.ListValueFrom(ctx, types.StringType, tagValues)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Tags = tagsList

	if g.EndpointHost != nil {
		m.EndpointHost = types.StringValue(*g.EndpointHost)
	} else {
		m.EndpointHost = types.StringNull()
	}
	if g.EndpointPort != nil {
		m.EndpointPort = types.Int64Value(int64(*g.EndpointPort))
	} else {
		m.EndpointPort = types.Int64Null()
	}
	if g.PublicKey != nil {
		m.PublicKey = types.StringValue(*g.PublicKey)
	} else {
		m.PublicKey = types.StringNull()
	}
	if g.PublicIPAddress != nil {
		m.PublicIPAddress = types.StringValue(*g.PublicIPAddress)
	} else {
		m.PublicIPAddress = types.StringNull()
	}
	return diags
}

// listToStrings converts a framework List into a Go slice. Null and unknown
// both collapse to nil so callers can hand the result straight to the client.
func listToStrings(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
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

// optStr returns a *string for an Optional value, nil when null/unknown so the
// API applies its own default.
func optStr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func (r *vpnGatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpnGatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcIDs, diags := listToStrings(ctx, plan.VpcIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tags, diags := listToStrings(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateVPNGateway(ctx, client.VPNGatewayCreateRequest{
		Name:         plan.Name.ValueString(),
		Region:       plan.Region.ValueString(),
		Plan:         plan.Plan.ValueString(),
		VpcIDs:       vpcIDs,
		PublicIPID:   optStr(plan.PublicIPID),
		PeerPoolCIDR: optStr(plan.PeerPoolCIDR),
		DNS:          optStr(plan.DNS),
		Tags:         tags,
	})
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VPN gateway already exists",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create VPN gateway",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &plan, created)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpnGatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpnGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetVPNGateway(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VPN gateway",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &state, got)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: every settable field has RequiresReplace, so the framework
// will never call this. Guard with a diagnostic in case someone changes the
// schema later without revisiting Update.
func (r *vpnGatewayResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"ccp_vpn_gateway has no mutable attributes; all changes force replacement. "+
			"Reaching Update means the schema and the implementation are out of sync — please report this as a provider bug.",
	)
}

func (r *vpnGatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpnGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteVPNGateway(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete VPN gateway",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Teardown is asynchronous: wait until the appliance is really gone so a
	// replace (destroy-then-create with the same name) doesn't race a 409.
	if err := client.PollUntilDeleted(ctx, 15*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetVPNGateway(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm VPN gateway deletion", err.Error())
	}
}

func (r *vpnGatewayResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
