// Package vpnpeer implements the ccp_vpn_peer Terraform resource.
//
// A VPN peer is a registered WireGuard client of a ccp_vpn_gateway. Two
// enrollment models are supported, selected by whether `public_key` is set:
//
//   - Model A — bring-your-own-key: the user supplies `public_key`. The server
//     never sees a private key; the returned `config` is a skeleton with no
//     `PrivateKey` line. `store_private_key`/`one_time` are irrelevant.
//   - Model B — server-generated: `public_key` is omitted. The server generates
//     a keypair and (when `store_private_key` is true, the default) returns a
//     ready-to-use `config` containing the private key. `one_time` (default
//     false) makes the config retrievable only once.
//
// CRUD semantics:
//   - Create : POST /v1/vpn/gateways/{gateway_id}/peers — returns id, ip,
//     public_key, model and config. `config` is returned ONLY here, so it is
//     persisted in state and never re-fetched.
//   - Read   : the API exposes no single-peer GET, so Read LISTS the gateway's
//     peers and filters by id (CLAUDE.md pitfall #6). The list response omits
//     `config`, so Read preserves the create-time `config`/`model` from state
//     rather than clobbering them.
//   - Delete : DELETE /v1/vpn/gateways/{gateway_id}/peers/{id}.
//
// `gateway_id` and `name` are immutable (the API has no peer-update endpoint),
// so both force replacement and Update is a guarded no-op.
package vpnpeer

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*vpnPeerResource)(nil)
	_ resource.ResourceWithConfigure   = (*vpnPeerResource)(nil)
	_ resource.ResourceWithImportState = (*vpnPeerResource)(nil)
)

// New returns a freshly-constructed ccp_vpn_peer resource. Wired in by
// provider.go via vpnpeer.New.
func New() resource.Resource {
	return &vpnPeerResource{}
}

type vpnPeerResource struct {
	client *client.Client
}

// vpnPeerResourceModel mirrors the schema below 1-to-1.
type vpnPeerResourceModel struct {
	ID              types.String `tfsdk:"id"`
	GatewayID       types.String `tfsdk:"gateway_id"`
	Name            types.String `tfsdk:"name"`
	PublicKey       types.String `tfsdk:"public_key"`
	StorePrivateKey types.Bool   `tfsdk:"store_private_key"`
	OneTime         types.Bool   `tfsdk:"one_time"`
	IP              types.String `tfsdk:"ip"`
	Model           types.String `tfsdk:"model"`
	Config          types.String `tfsdk:"config"`
}

var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\- ]+$`)

func (r *vpnPeerResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vpn_peer"
}

func (r *vpnPeerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VPN peer — a registered WireGuard client of a " +
			"`ccp_vpn_gateway`. Supply `public_key` for bring-your-own-key enrollment (Model A); omit it " +
			"to have the platform generate a keypair and return a ready-to-use `config` containing the " +
			"private key (Model B). The CETIC Cloud API has no peer-update endpoint, so any change to " +
			"`gateway_id` or `name` forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the peer.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"gateway_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the `ccp_vpn_gateway` this peer connects to. " +
					"Immutable — changing forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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
			"public_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard public key of the client (Model A, bring-your-own-key). " +
					"When set, the server never generates or stores a private key. When omitted, the server " +
					"generates a keypair (Model B) and this attribute is populated from the response. " +
					"Immutable — changing forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"store_private_key": schema.BoolAttribute{
				MarkdownDescription: "Model B only: when `true` (default) the server-generated private key is " +
					"embedded in `config`. Ignored when `public_key` is set. Immutable — forces replacement.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"one_time": schema.BoolAttribute{
				MarkdownDescription: "Model B only: when `true` the generated `config` is retrievable only " +
					"once (at create). Defaults to `false`. Ignored when `public_key` is set. " +
					"Immutable — forces replacement.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"ip": schema.StringAttribute{
				MarkdownDescription: "Tunnel IP assigned to the peer from the gateway's peer pool.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"model": schema.StringAttribute{
				MarkdownDescription: "Enrollment model resolved by the server: `byok` (Model A) or " +
					"`generated` (Model B).",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"config": schema.StringAttribute{
				MarkdownDescription: "Full WireGuard client configuration. In Model B (server-generated) it " +
					"contains the peer's private key, so it is **sensitive**. Returned only at create time " +
					"and preserved in state thereafter — the API does not re-expose it on read.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *vpnPeerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// applyCreateToModel maps the full create response (id, ip, public_key, model,
// config) onto the model. Only call this from Create — the list response used
// by Read omits config/model, so Read must NOT use this helper.
func applyCreateToModel(m *vpnPeerResourceModel, p *client.VPNPeer) {
	m.ID = types.StringValue(p.ID)
	m.IP = types.StringValue(p.IP)
	m.PublicKey = types.StringValue(p.PublicKey)
	m.Model = types.StringValue(p.Model)
	m.Config = types.StringValue(p.Config)
}

func (r *vpnPeerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpnPeerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.VPNPeerCreateRequest{
		Name: plan.Name.ValueString(),
	}
	// Model A vs Model B: presence of public_key selects bring-your-own-key.
	if !plan.PublicKey.IsNull() && !plan.PublicKey.IsUnknown() && plan.PublicKey.ValueString() != "" {
		pk := plan.PublicKey.ValueString()
		createReq.PublicKey = &pk
	} else {
		// Model B knobs are only meaningful when the server generates the key.
		if !plan.StorePrivateKey.IsNull() && !plan.StorePrivateKey.IsUnknown() {
			v := plan.StorePrivateKey.ValueBool()
			createReq.StorePrivateKey = &v
		}
		if !plan.OneTime.IsNull() && !plan.OneTime.IsUnknown() {
			v := plan.OneTime.ValueBool()
			createReq.OneTime = &v
		}
	}

	created, err := r.client.CreateVPNPeer(ctx, plan.GatewayID.ValueString(), createReq)
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VPN peer already exists",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create VPN peer",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	applyCreateToModel(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpnPeerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpnPeerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No single-peer GET: list and filter by id (CLAUDE.md pitfall #6).
	got, err := r.client.GetVPNPeer(ctx, state.GatewayID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VPN peer",
			fmt.Sprintf("CETIC Cloud API error for gateway %s peer %s: %s",
				state.GatewayID.ValueString(), state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Refresh the fields the list response actually carries. Deliberately do
	// NOT touch `config` (create-only secret) nor `model` (absent from the list
	// payload) — keep the create-time values already in state.
	state.IP = types.StringValue(got.IP)
	if got.PublicKey != "" {
		state.PublicKey = types.StringValue(got.PublicKey)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: every settable field has RequiresReplace.
func (r *vpnPeerResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"ccp_vpn_peer has no mutable attributes; all changes force replacement. "+
			"Reaching Update means the schema and the implementation are out of sync — please report this as a provider bug.",
	)
}

func (r *vpnPeerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpnPeerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteVPNPeer(ctx, state.GatewayID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete VPN peer",
			fmt.Sprintf("CETIC Cloud API error for gateway %s peer %s: %s",
				state.GatewayID.ValueString(), state.ID.ValueString(), err.Error()),
		)
	}
}

// ImportState parses `<gateway_id>/<peer_id>`. The create-only `config` cannot
// be recovered on import (the API never re-exposes it).
func (r *vpnPeerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<gateway_id>/<peer_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("gateway_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
