// Package vpnpolicy implements the ccp_vpn_policy Terraform resource.
//
// A VPN policy is the access policy of a single ccp_vpn_gateway — a SINGLETON
// per gateway (one policy per gateway, keyed by gateway_id). It maps peer
// client names to logical groups (`groups`) and lists firewall rules (`rules`)
// gating which groups may reach which CIDRs on which ports/protocols.
//
// API contract (#306):
//   - GET /v1/vpn/gateways/{gateway_id}/policy → {groups, rules}
//   - PUT /v1/vpn/gateways/{gateway_id}/policy with the same body → replaces the
//     policy and returns it. Requires ADMIN role on the API token.
//   - There is NO DELETE endpoint. Terraform Delete therefore clears the policy
//     by PUTing an empty body ({"groups":{}, "rules":[]}), which returns the
//     gateway to its default full-access behaviour.
//
// CRUD semantics:
//   - Create : PUT the policy.
//   - Read   : GET the policy and map groups + rules back. 404 ⇒ the gateway is
//     gone, remove from state. An empty server-side policy is reflected as-is.
//   - Update : PUT the policy (groups/rules are mutable in place — only
//     gateway_id is ForceNew).
//   - Delete : PUT an empty policy.
//
// The policy has no id of its own, so the resource id is the gateway_id.
package vpnpolicy

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*vpnPolicyResource)(nil)
	_ resource.ResourceWithConfigure   = (*vpnPolicyResource)(nil)
	_ resource.ResourceWithImportState = (*vpnPolicyResource)(nil)
)

// New returns a freshly-constructed ccp_vpn_policy resource. Wired in by
// provider.go via vpnpolicy.New.
func New() resource.Resource {
	return &vpnPolicyResource{}
}

type vpnPolicyResource struct {
	client *client.Client
}

// vpnPolicyResourceModel mirrors the schema below 1-to-1.
type vpnPolicyResourceModel struct {
	ID        types.String `tfsdk:"id"`
	GatewayID types.String `tfsdk:"gateway_id"`
	Groups    types.Map    `tfsdk:"groups"`
	Rules     types.List   `tfsdk:"rules"`
}

// vpnPolicyRuleModel mirrors one element of the `rules` ListNestedAttribute.
type vpnPolicyRuleModel struct {
	FromGroup types.String `tfsdk:"from_group"`
	ToCidr    types.String `tfsdk:"to_cidr"`
	Ports     types.List   `tfsdk:"ports"`
	Proto     types.String `tfsdk:"proto"`
}

func (r *vpnPolicyResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vpn_policy"
}

func (r *vpnPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the access policy of a CETIC Cloud VPN gateway (`ccp_vpn_gateway`). " +
			"A policy is a singleton per gateway: it assigns peer client names to logical `groups` and lists " +
			"`rules` gating which group may reach which CIDR on which ports/protocol. Clearing the policy " +
			"(empty `groups` and `rules`, or destroying this resource) returns the gateway to its default " +
			"full-access behaviour. Replacing the policy requires an API token with the **ADMIN** role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier. A policy has no id of its own, so this mirrors " +
					"`gateway_id`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"gateway_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the `ccp_vpn_gateway` this policy governs. " +
					"Immutable — changing forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"groups": schema.MapAttribute{
				MarkdownDescription: "Map of peer client name → list of logical group names that client " +
					"belongs to. Mutable in place.",
				Required:    true,
				ElementType: types.ListType{ElemType: types.StringType},
			},
			"rules": schema.ListNestedAttribute{
				MarkdownDescription: "Ordered list of access rules. Each rule allows a logical group to reach " +
					"a CIDR on the given ports/protocol. Mutable in place.",
				Required: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"from_group": schema.StringAttribute{
							MarkdownDescription: "Logical group (from `groups`) the rule applies to.",
							Required:            true,
						},
						"to_cidr": schema.StringAttribute{
							MarkdownDescription: "Destination CIDR the group is allowed to reach.",
							Required:            true,
						},
						"ports": schema.ListAttribute{
							MarkdownDescription: "Destination ports. Omit (or empty) for all ports.",
							Optional:            true,
							ElementType:         types.Int64Type,
						},
						"proto": schema.StringAttribute{
							MarkdownDescription: "Protocol: `tcp` (default), `udp`, or `any`.",
							Optional:            true,
							Computed:            true,
							Default:             stringdefault.StaticString("tcp"),
							Validators: []validator.String{
								stringvalidator.OneOf("tcp", "udp", "any"),
							},
						},
					},
				},
			},
		},
	}
}

func (r *vpnPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// modelToPolicy converts the Terraform model into an API VPNPolicy body.
func modelToPolicy(ctx context.Context, m *vpnPolicyResourceModel) (client.VPNPolicy, diag.Diagnostics) {
	var diags diag.Diagnostics
	policy := client.VPNPolicy{
		Groups: map[string][]string{},
		Rules:  []client.VPNPolicyRule{},
	}

	if !m.Groups.IsNull() && !m.Groups.IsUnknown() {
		raw := map[string][]string{}
		diags.Append(m.Groups.ElementsAs(ctx, &raw, false)...)
		if diags.HasError() {
			return policy, diags
		}
		policy.Groups = raw
	}

	if !m.Rules.IsNull() && !m.Rules.IsUnknown() {
		var rules []vpnPolicyRuleModel
		diags.Append(m.Rules.ElementsAs(ctx, &rules, false)...)
		if diags.HasError() {
			return policy, diags
		}
		for i := range rules {
			rule := client.VPNPolicyRule{
				FromGroup: rules[i].FromGroup.ValueString(),
				ToCidr:    rules[i].ToCidr.ValueString(),
				Proto:     rules[i].Proto.ValueString(),
				Ports:     []int64{},
			}
			if !rules[i].Ports.IsNull() && !rules[i].Ports.IsUnknown() {
				var ports []int64
				diags.Append(rules[i].Ports.ElementsAs(ctx, &ports, false)...)
				if diags.HasError() {
					return policy, diags
				}
				rule.Ports = ports
			}
			policy.Rules = append(policy.Rules, rule)
		}
	}

	return policy, diags
}

// applyToModel maps an API VPNPolicy back onto the Terraform model. The resource
// id mirrors gateway_id.
func applyToModel(ctx context.Context, m *vpnPolicyResourceModel, p *client.VPNPolicy) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = m.GatewayID

	groups := p.Groups
	if groups == nil {
		groups = map[string][]string{}
	}
	groupsVal, d := types.MapValueFrom(ctx, types.ListType{ElemType: types.StringType}, groups)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Groups = groupsVal

	ruleObjType := types.ObjectType{AttrTypes: ruleObjectAttrTypes()}
	ruleVals := make([]vpnPolicyRuleModel, 0, len(p.Rules))
	for i := range p.Rules {
		rm := vpnPolicyRuleModel{
			FromGroup: types.StringValue(p.Rules[i].FromGroup),
			ToCidr:    types.StringValue(p.Rules[i].ToCidr),
			Proto:     types.StringValue(p.Rules[i].Proto),
		}
		if len(p.Rules[i].Ports) > 0 {
			portsVal, pd := types.ListValueFrom(ctx, types.Int64Type, p.Rules[i].Ports)
			diags.Append(pd...)
			if diags.HasError() {
				return diags
			}
			rm.Ports = portsVal
		} else {
			rm.Ports = types.ListNull(types.Int64Type)
		}
		ruleVals = append(ruleVals, rm)
	}
	rulesVal, d := types.ListValueFrom(ctx, ruleObjType, ruleVals)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Rules = rulesVal

	return diags
}

// ruleObjectAttrTypes is the attribute-type map of a single rule object, used
// when building the framework List value in applyToModel.
func ruleObjectAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"from_group": types.StringType,
		"to_cidr":    types.StringType,
		"ports":      types.ListType{ElemType: types.Int64Type},
		"proto":      types.StringType,
	}
}

func (r *vpnPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpnPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, diags := modelToPolicy(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	stored, err := r.client.PutVPNPolicy(ctx, plan.GatewayID.ValueString(), policy)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to set VPN policy",
			fmt.Sprintf("CETIC Cloud API error for gateway %s: %s", plan.GatewayID.ValueString(), err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &plan, stored)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpnPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpnPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetVPNPolicy(ctx, state.GatewayID.ValueString())
	if err != nil {
		// 404 here means the gateway itself is gone — drop the policy from state.
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VPN policy",
			fmt.Sprintf("CETIC Cloud API error for gateway %s: %s", state.GatewayID.ValueString(), err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &state, got)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vpnPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan vpnPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, diags := modelToPolicy(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	stored, err := r.client.PutVPNPolicy(ctx, plan.GatewayID.ValueString(), policy)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to update VPN policy",
			fmt.Sprintf("CETIC Cloud API error for gateway %s: %s", plan.GatewayID.ValueString(), err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &plan, stored)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the policy by PUTing an empty body — there is no DELETE endpoint
// and clearing the policy returns the gateway to default full access.
func (r *vpnPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpnPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	empty := client.VPNPolicy{Groups: map[string][]string{}, Rules: []client.VPNPolicyRule{}}
	if _, err := r.client.PutVPNPolicy(ctx, state.GatewayID.ValueString(), empty); err != nil {
		// The gateway is already gone — nothing left to clear.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to clear VPN policy",
			fmt.Sprintf("CETIC Cloud API error for gateway %s: %s", state.GatewayID.ValueString(), err.Error()),
		)
	}
}

// ImportState takes the gateway_id as the import id (== resource id).
func (r *vpnPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("gateway_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
