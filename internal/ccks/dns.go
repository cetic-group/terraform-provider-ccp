package ccks

// Private DNS and custom DNS of a CCKS cluster (cetic-cloud-platform#2181),
// shared by the `ccp_k8s_cluster` resource and data source.

import (
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// CustomDNSMax is the number of domains the platform accepts (Q15).
const CustomDNSMax = 5

// CustomDNSEntryAttrTypes is one `custom_dns` element.
var CustomDNSEntryAttrTypes = map[string]attr.Type{
	"domain":  types.StringType,
	"servers": types.ListType{ElemType: types.StringType},
}

// CustomDNSElemType is the element type of `custom_dns`.
var CustomDNSElemType = types.ObjectType{AttrTypes: CustomDNSEntryAttrTypes}

var privateDNSZoneAttrTypes = map[string]attr.Type{
	"name":   types.StringType,
	"status": types.StringType,
}

var privateDNSExcludedAttrTypes = map[string]attr.Type{
	"zone":   types.StringType,
	"reason": types.StringType,
}

var privateDNSCustomDomainAttrTypes = map[string]attr.Type{
	"domain":  types.StringType,
	"servers": types.ListType{ElemType: types.StringType},
	"status":  types.StringType,
	"reason":  types.StringType,
}

// PrivateDNSAttrTypes is the `private_dns` object.
var PrivateDNSAttrTypes = map[string]attr.Type{
	"status":         types.StringType,
	"resolver_ip":    types.StringType,
	"zones":          types.ListType{ElemType: types.ObjectType{AttrTypes: privateDNSZoneAttrTypes}},
	"excluded":       types.ListType{ElemType: types.ObjectType{AttrTypes: privateDNSExcludedAttrTypes}},
	"custom_domains": types.ListType{ElemType: types.ObjectType{AttrTypes: privateDNSCustomDomainAttrTypes}},
	"pending_reason": types.StringType,
	"applied_at":     types.StringType,
	"checked_at":     types.StringType,
	"error":          types.StringType,
	"error_code":     types.StringType,
}

// PrivateDNSDescriptions documents each `private_dns` attribute, for the
// resource and the data source alike.
var PrivateDNSDescriptions = map[string]string{
	"status": "`applied` (everything is in CoreDNS), `pending` (a zone or domain is waiting), " +
		"`error` (see `error` and `error_code`) or `unsupported`.",
	"resolver_ip":    "Address of the VPC DNS service the private zones are forwarded to.",
	"zones":          "Private DNS zones of the VPC, added automatically: `name` and `status` (`applied` or `pending`).",
	"excluded":       "Zones of the VPC left out, with their `reason` (`denylisted`, `cluster_domain`, `cluster_range`, `client_conflict`, `invalid_name`, `custom_dns_conflict`).",
	"custom_domains": "Custom DNS domains as applied: `domain`, `servers`, `status` (`applied`, `pending` or `error`) and `reason`.",
	"pending_reason": "Why the zones are waiting: `resolver_not_ready` or `resolver_unavailable`.",
	"applied_at":     "Last time the platform wrote the configuration into the cluster.",
	"checked_at":     "Last time the platform checked the configuration in the cluster.",
	"error":          "Readable error message, when `status` is `error`.",
	"error_code":     "Machine code of the error (e.g. `cluster_unreachable`, `client_coredns_config_invalid`).",
}

func strOrNull(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func stringList(in []string) (types.List, diag.Diagnostics) {
	vs := make([]attr.Value, 0, len(in))
	for _, s := range in {
		vs = append(vs, types.StringValue(s))
	}
	return types.ListValue(types.StringType, vs)
}

// PrivateDNSValue is the `private_dns` value of an API response (null when
// the API reports none).
func PrivateDNSValue(p *client.K8sClusterPrivateDNS) (types.Object, diag.Diagnostics) {
	if p == nil {
		return types.ObjectNull(PrivateDNSAttrTypes), nil
	}
	var diags diag.Diagnostics
	zoneT := types.ObjectType{AttrTypes: privateDNSZoneAttrTypes}
	exclT := types.ObjectType{AttrTypes: privateDNSExcludedAttrTypes}
	domT := types.ObjectType{AttrTypes: privateDNSCustomDomainAttrTypes}

	zones := make([]attr.Value, 0, len(p.Zones))
	for _, z := range p.Zones {
		o, d := types.ObjectValue(privateDNSZoneAttrTypes, map[string]attr.Value{
			"name": types.StringValue(z.Name), "status": types.StringValue(z.Status),
		})
		diags.Append(d...)
		zones = append(zones, o)
	}
	excl := make([]attr.Value, 0, len(p.Excluded))
	for _, e := range p.Excluded {
		o, d := types.ObjectValue(privateDNSExcludedAttrTypes, map[string]attr.Value{
			"zone": types.StringValue(e.Zone), "reason": types.StringValue(e.Reason),
		})
		diags.Append(d...)
		excl = append(excl, o)
	}
	doms := make([]attr.Value, 0, len(p.CustomDomains))
	for _, c := range p.CustomDomains {
		servers, d := stringList(c.Servers)
		diags.Append(d...)
		o, d := types.ObjectValue(privateDNSCustomDomainAttrTypes, map[string]attr.Value{
			"domain": types.StringValue(c.Domain), "servers": servers,
			"status": types.StringValue(c.Status), "reason": strOrNull(c.Reason),
		})
		diags.Append(d...)
		doms = append(doms, o)
	}
	zl, d := types.ListValue(zoneT, zones)
	diags.Append(d...)
	el, d := types.ListValue(exclT, excl)
	diags.Append(d...)
	dl, d := types.ListValue(domT, doms)
	diags.Append(d...)
	out, d := types.ObjectValue(PrivateDNSAttrTypes, map[string]attr.Value{
		"status":         types.StringValue(p.Status),
		"resolver_ip":    strOrNull(p.ResolverIP),
		"zones":          zl,
		"excluded":       el,
		"custom_domains": dl,
		"pending_reason": strOrNull(p.PendingReason),
		"applied_at":     strOrNull(p.AppliedAt),
		"checked_at":     strOrNull(p.CheckedAt),
		"error":          strOrNull(p.Error),
		"error_code":     strOrNull(p.ErrorCode),
	})
	diags.Append(d...)
	return out, diags
}

// NormalizeCustomDNS mirrors the API normalisation
// (services/ccks_dns_prive.valider_custom_dns): domain trimmed, lower case,
// without its final dot; servers trimmed; order kept (the first server is
// the primary).
func NormalizeCustomDNS(in []client.K8sCustomDNSEntry) []client.K8sCustomDNSEntry {
	out := make([]client.K8sCustomDNSEntry, 0, len(in))
	for _, e := range in {
		n := client.K8sCustomDNSEntry{
			Domain:  strings.TrimRight(strings.ToLower(strings.TrimSpace(e.Domain)), "."),
			Servers: make([]string, 0, len(e.Servers)),
		}
		for _, s := range e.Servers {
			n.Servers = append(n.Servers, strings.TrimSpace(s))
		}
		out = append(out, n)
	}
	return out
}

// CustomDNSSame compares two lists once normalised.
func CustomDNSSame(a, b []client.K8sCustomDNSEntry) bool {
	na, nb := NormalizeCustomDNS(a), NormalizeCustomDNS(b)
	if len(na) != len(nb) {
		return false
	}
	for i := range na {
		if na[i].Domain != nb[i].Domain || len(na[i].Servers) != len(nb[i].Servers) {
			return false
		}
		for j := range na[i].Servers {
			if na[i].Servers[j] != nb[i].Servers[j] {
				return false
			}
		}
	}
	return true
}

// CustomDNSListValue is the list value of API entries (an empty list for
// none: the data source always reports a list).
func CustomDNSListValue(in []client.K8sCustomDNSEntry) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	elems := make([]attr.Value, 0, len(in))
	for _, e := range in {
		servers, d := stringList(e.Servers)
		diags.Append(d...)
		o, d := types.ObjectValue(CustomDNSEntryAttrTypes, map[string]attr.Value{
			"domain": types.StringValue(e.Domain), "servers": servers,
		})
		diags.Append(d...)
		elems = append(elems, o)
	}
	l, d := types.ListValue(CustomDNSElemType, elems)
	diags.Append(d...)
	return l, diags
}
