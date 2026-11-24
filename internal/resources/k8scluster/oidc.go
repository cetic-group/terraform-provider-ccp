package k8scluster

// `oidc` — SSO OIDC of the apiserver (`PUT/DELETE /v1/k8s/clusters/{id}/oidc`,
// cetic-cloud-platform#2143).
//
// Authoritative: set, it is the configuration of the cluster; removed, the
// SSO is removed (DELETE). A changed configuration restarts the apiserver
// (an API cut on a one-replica plan), so the PUT is sent ONLY when the
// configuration differs from the state once both are put in the canonical
// form the API stores (trimmed strings, empty groups claim = no groups,
// scopes without duplicates nor `openid`, PEM blocks re-joined). The state
// keeps the spelling of the configuration when it is the same configuration.

import (
	"context"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var oidcAttrTypes = map[string]attr.Type{
	"issuer_url":      types.StringType,
	"client_id":       types.StringType,
	"username_claim":  types.StringType,
	"username_prefix": types.StringType,
	"groups_claim":    types.StringType,
	"groups_prefix":   types.StringType,
	"extra_scopes":    types.ListType{ElemType: types.StringType},
	"ca_pem":          types.StringType,
}

type oidcModel struct {
	IssuerURL      types.String `tfsdk:"issuer_url"`
	ClientID       types.String `tfsdk:"client_id"`
	UsernameClaim  types.String `tfsdk:"username_claim"`
	UsernamePrefix types.String `tfsdk:"username_prefix"`
	GroupsClaim    types.String `tfsdk:"groups_claim"`
	GroupsPrefix   types.String `tfsdk:"groups_prefix"`
	ExtraScopes    types.List   `tfsdk:"extra_scopes"`
	CaPEM          types.String `tfsdk:"ca_pem"`
}

func oidcSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "SSO OIDC of the apiserver: users sign in with the organisation's identity " +
			"provider (kubeconfig `mode=sso`, kubelogin). Set, it is the configuration of the cluster; " +
			"removed, the SSO is removed. **Changing it restarts the apiserver** (a short API cut on the " +
			"Starter plan); the provider only sends it when it actually changes. The certificate " +
			"kubeconfig remains the cluster-admin access. Requires an active cluster.",
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"issuer_url": schema.StringAttribute{
				MarkdownDescription: "Exact `iss` of the tokens: `https://`, port 443, public host, no query nor fragment.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(9, 512)},
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "PUBLIC client of the identity provider (PKCE / device flow, no secret): the expected audience.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"username_claim": schema.StringAttribute{
				MarkdownDescription: "Claim giving the Kubernetes user name. Default `email`.",
				Optional:            true, Computed: true,
				Default: stringdefault.StaticString("email"),
			},
			"username_prefix": schema.StringAttribute{
				MarkdownDescription: "Prefix of the Kubernetes user name (`oidc:alice@example.com`); `\"\"` for none. Default `oidc:`.",
				Optional:            true, Computed: true,
				Default: stringdefault.StaticString("oidc:"),
			},
			"groups_claim": schema.StringAttribute{
				MarkdownDescription: "Claim giving the groups; `\"\"` for no groups (RBAC by user name only). Default `groups`.",
				Optional:            true, Computed: true,
				Default: stringdefault.StaticString("groups"),
			},
			"groups_prefix": schema.StringAttribute{
				MarkdownDescription: "Prefix of every group; `\"\"` for none (non-empty: the claim must be a list). Default `oidc:`.",
				Optional:            true, Computed: true,
				Default: stringdefault.StaticString("oidc:"),
			},
			"extra_scopes": schema.ListAttribute{
				MarkdownDescription: "Scopes kubelogin asks for besides `openid` (at most 10). Default none.",
				ElementType:         types.StringType,
				Optional:            true, Computed: true,
				Default:    listdefault.StaticValue(types.ListValueMust(types.StringType, []attr.Value{})),
				Validators: []validator.List{listvalidator.SizeAtMost(10)},
			},
			"ca_pem": schema.StringAttribute{
				MarkdownDescription: "PEM certificates of the identity provider's CA, when it is not signed by a public CA.",
				Optional:            true,
			},
		},
	}
}

// oidcFromObject is the PUT body of a configured `oidc`.
func oidcFromObject(ctx context.Context, o types.Object) (client.K8sClusterOIDC, diag.Diagnostics) {
	var m oidcModel
	diags := o.As(ctx, &m, basetypes.ObjectAsOptions{UnhandledNullAsEmpty: true, UnhandledUnknownAsEmpty: true})
	out := client.K8sClusterOIDC{
		IssuerURL:      m.IssuerURL.ValueString(),
		ClientID:       m.ClientID.ValueString(),
		UsernameClaim:  m.UsernameClaim.ValueString(),
		UsernamePrefix: m.UsernamePrefix.ValueString(),
		GroupsPrefix:   m.GroupsPrefix.ValueString(),
		ExtraScopes:    []string{},
	}
	if known(m.GroupsClaim) && strings.TrimSpace(m.GroupsClaim.ValueString()) != "" {
		v := m.GroupsClaim.ValueString()
		out.GroupsClaim = &v
	}
	if known(m.ExtraScopes) {
		var scopes []string
		diags.Append(m.ExtraScopes.ElementsAs(ctx, &scopes, false)...)
		if scopes != nil {
			out.ExtraScopes = scopes
		}
	}
	if known(m.CaPEM) && strings.TrimSpace(m.CaPEM.ValueString()) != "" {
		v := m.CaPEM.ValueString()
		out.CaPEM = &v
	}
	return out, diags
}

// canonicalPEM keeps the non-blank lines, trimmed: the API re-joins the PEM
// blocks with single newlines.
func canonicalPEM(p *string) string {
	if p == nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(*p, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// oidcCanonical mirrors the API normalisation (services/ccks_oidc.normaliser).
func oidcCanonical(o client.K8sClusterOIDC) client.K8sClusterOIDC {
	out := client.K8sClusterOIDC{
		IssuerURL:      strings.TrimSpace(o.IssuerURL),
		ClientID:       strings.TrimSpace(o.ClientID),
		UsernameClaim:  strings.TrimSpace(o.UsernameClaim),
		UsernamePrefix: strings.TrimSpace(o.UsernamePrefix),
		GroupsPrefix:   strings.TrimSpace(o.GroupsPrefix),
		ExtraScopes:    []string{},
	}
	if out.UsernameClaim == "" {
		out.UsernameClaim = "email"
	}
	if o.GroupsClaim != nil {
		if g := strings.TrimSpace(*o.GroupsClaim); g != "" {
			out.GroupsClaim = &g
		}
	}
	seen := map[string]bool{}
	for _, s := range o.ExtraScopes {
		s = strings.TrimSpace(s)
		if s != "" && s != "openid" && !seen[s] {
			seen[s] = true
			out.ExtraScopes = append(out.ExtraScopes, s)
		}
	}
	if pem := canonicalPEM(o.CaPEM); pem != "" {
		out.CaPEM = &pem
	}
	return out
}

func oidcSame(a, b client.K8sClusterOIDC) bool {
	ca, cb := oidcCanonical(a), oidcCanonical(b)
	str := func(p *string) string {
		if p == nil {
			return "\x00"
		}
		return *p
	}
	if ca.IssuerURL != cb.IssuerURL || ca.ClientID != cb.ClientID ||
		ca.UsernameClaim != cb.UsernameClaim || ca.UsernamePrefix != cb.UsernamePrefix ||
		ca.GroupsPrefix != cb.GroupsPrefix || str(ca.GroupsClaim) != str(cb.GroupsClaim) ||
		str(ca.CaPEM) != str(cb.CaPEM) || len(ca.ExtraScopes) != len(cb.ExtraScopes) {
		return false
	}
	for i := range ca.ExtraScopes {
		if ca.ExtraScopes[i] != cb.ExtraScopes[i] {
			return false
		}
	}
	return true
}

// oidcObjectsSame compares two `oidc` values (null = no SSO).
func oidcObjectsSame(ctx context.Context, a, b types.Object) bool {
	if a.IsNull() || b.IsNull() {
		return a.IsNull() && b.IsNull()
	}
	if a.IsUnknown() || b.IsUnknown() {
		return false
	}
	oa, d1 := oidcFromObject(ctx, a)
	ob, d2 := oidcFromObject(ctx, b)
	return !d1.HasError() && !d2.HasError() && oidcSame(oa, ob)
}

// oidcValue is the state value of the API configuration: null without SSO,
// the prior value when it is the same configuration, else the API's.
func oidcValue(ctx context.Context, api *client.K8sClusterOIDC, prior types.Object) (types.Object, diag.Diagnostics) {
	if api == nil {
		return types.ObjectNull(oidcAttrTypes), nil
	}
	if known(prior) {
		if p, d := oidcFromObject(ctx, prior); !d.HasError() && oidcSame(p, *api) {
			return prior, nil
		}
	}
	var diags diag.Diagnostics
	scopes := make([]attr.Value, 0, len(api.ExtraScopes))
	for _, s := range api.ExtraScopes {
		scopes = append(scopes, types.StringValue(s))
	}
	sl, d := types.ListValue(types.StringType, scopes)
	diags.Append(d...)
	groups := ""
	if api.GroupsClaim != nil {
		groups = *api.GroupsClaim
	}
	ca := types.StringNull()
	if api.CaPEM != nil && *api.CaPEM != "" {
		ca = types.StringValue(*api.CaPEM)
	}
	out, d := types.ObjectValue(oidcAttrTypes, map[string]attr.Value{
		"issuer_url":      types.StringValue(api.IssuerURL),
		"client_id":       types.StringValue(api.ClientID),
		"username_claim":  types.StringValue(api.UsernameClaim),
		"username_prefix": types.StringValue(api.UsernamePrefix),
		"groups_claim":    types.StringValue(groups),
		"groups_prefix":   types.StringValue(api.GroupsPrefix),
		"extra_scopes":    sl,
		"ca_pem":          ca,
	})
	diags.Append(d...)
	return out, diags
}
