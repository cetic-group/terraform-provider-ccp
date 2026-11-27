package k8scluster

// `custom_dns` — custom DNS of the cluster's CoreDNS
// (`PUT /v1/k8s/clusters/{id}/custom-dns`, cetic-cloud-platform#2181) — and
// the computed `private_dns` state.
//
// Authoritative, like `oidc`: set, it is the list of the cluster; removed (or
// `[]`), the custom DNS is removed. Sent with the creation request (the API
// validates it before any provisioning: an invalid line fails the apply with
// nothing created), then by the PUT, only when the list differs from the state
// once normalised like the API does (domain in lower case without its final
// dot, servers trimmed). No restart: CoreDNS reloads it within 2 minutes.

import (
	"context"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/ccks"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ipv4 is a dotted IPv4 address without leading zeros; the API checks the
// rest (unicast, inside a subnet of the VPC, not reserved by the platform).
var ipv4 = regexp.MustCompile(`^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$`)

func customDNSSchema() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		MarkdownDescription: "Custom DNS of the cluster's CoreDNS: each `domain` is resolved by 1 or 2 DNS servers " +
			"of yours, in a subnet of the cluster's VPC, that accept queries from the nodes on port 53 (with " +
			"two servers, the first is the primary, the second a fallback). At most 5 domains. Set, it is the " +
			"list of the cluster; removed, the custom DNS is removed. Applied without restart (CoreDNS reloads " +
			"it within 2 minutes); the private DNS zones of the VPC are added automatically and must not be " +
			"listed here. Validated by the platform (422 with a reason per line, nothing written).",
		Optional: true,
		Validators: []validator.List{
			listvalidator.SizeAtMost(ccks.CustomDNSMax),
		},
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"domain": schema.StringAttribute{
					MarkdownDescription: "Domain (e.g. `corp.example.com`), resolved with all its subdomains. " +
						"Stored in lower case without its final dot.",
					Required:   true,
					Validators: []validator.String{stringvalidator.LengthBetween(1, 253)},
				},
				"servers": schema.ListAttribute{
					MarkdownDescription: "1 or 2 IPv4 addresses of DNS servers in a subnet of the cluster's VPC.",
					ElementType:         types.StringType,
					Required:            true,
					Validators: []validator.List{
						listvalidator.SizeBetween(1, 2),
						listvalidator.UniqueValues(),
						listvalidator.ValueStringsAre(stringvalidator.RegexMatches(ipv4, "must be an IPv4 address")),
					},
				},
			},
		},
	}
}

// privateDNSSchema is the computed state of the private DNS. No
// UseStateForUnknown: it moves on its own (zones applied, resolver ready), a
// pinned prior value would make the apply inconsistent.
func privateDNSSchema() schema.SingleNestedAttribute {
	d := ccks.PrivateDNSDescriptions
	str := func(name string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: d[name], Computed: true}
	}
	return schema.SingleNestedAttribute{
		MarkdownDescription: "State of the private DNS in the cluster's CoreDNS: private zones of the VPC DNS " +
			"service (automatic) and `custom_dns`, per zone and per domain. `null` when the VPC has no private " +
			"DNS zone and no custom DNS is set.",
		Computed: true,
		Attributes: map[string]schema.Attribute{
			"status":      str("status"),
			"resolver_ip": str("resolver_ip"),
			"zones": schema.ListNestedAttribute{
				MarkdownDescription: d["zones"], Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name":   schema.StringAttribute{Computed: true, MarkdownDescription: "Zone name."},
					"status": schema.StringAttribute{Computed: true, MarkdownDescription: "`applied` or `pending`."},
				}},
			},
			"excluded": schema.ListNestedAttribute{
				MarkdownDescription: d["excluded"], Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"zone":   schema.StringAttribute{Computed: true, MarkdownDescription: "Zone name."},
					"reason": schema.StringAttribute{Computed: true, MarkdownDescription: "Why it is left out."},
				}},
			},
			"custom_domains": schema.ListNestedAttribute{
				MarkdownDescription: d["custom_domains"], Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"domain":  schema.StringAttribute{Computed: true, MarkdownDescription: "Domain."},
					"servers": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "DNS servers."},
					"status":  schema.StringAttribute{Computed: true, MarkdownDescription: "`applied`, `pending` or `error`."},
					"reason":  schema.StringAttribute{Computed: true, MarkdownDescription: "Reason of an `error`."},
				}},
			},
			"pending_reason": str("pending_reason"),
			"applied_at":     str("applied_at"),
			"checked_at":     str("checked_at"),
			"error":          str("error"),
			"error_code":     str("error_code"),
		},
	}
}

// customDNSFromList is the API body of a `custom_dns` value (nil for null).
func customDNSFromList(ctx context.Context, l types.List) ([]client.K8sCustomDNSEntry, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := []client.K8sCustomDNSEntry{}
	if !known(l) {
		return out, diags
	}
	var objs []types.Object
	diags.Append(l.ElementsAs(ctx, &objs, false)...)
	for _, o := range objs {
		attrs := o.Attributes()
		e := client.K8sCustomDNSEntry{Servers: []string{}}
		if v, ok := attrs["domain"].(types.String); ok {
			e.Domain = v.ValueString()
		}
		if v, ok := attrs["servers"].(types.List); ok && known(v) {
			diags.Append(v.ElementsAs(ctx, &e.Servers, false)...)
		}
		out = append(out, e)
	}
	return out, diags
}

// customDNSValue is the state value of the API list: the prior value when it
// is the same list (configured spelling kept, null kept for "none"), null for
// an empty API list, else the API's.
func customDNSValue(ctx context.Context, api []client.K8sCustomDNSEntry, prior types.List) (types.List, diag.Diagnostics) {
	if known(prior) {
		if p, d := customDNSFromList(ctx, prior); !d.HasError() && ccks.CustomDNSSame(p, api) {
			return prior, nil
		}
	}
	if len(api) == 0 {
		return types.ListNull(ccks.CustomDNSElemType), nil
	}
	return ccks.CustomDNSListValue(api)
}

// applyCustomDNS sends the PUT when the planned list differs from current
// once normalised — never otherwise. Like `oidc`, the cluster is waited
// `active` first (an automatic maintenance may be running). Returns the
// cluster read after the call, nil when nothing was sent.
func (r *k8sResource) applyCustomDNS(ctx context.Context, id string, planned types.List, current []client.K8sCustomDNSEntry, activeTimeout time.Duration) (*client.K8sCluster, diag.Diagnostics) {
	var diags diag.Diagnostics
	if planned.IsUnknown() {
		return nil, diags
	}
	body, d := customDNSFromList(ctx, planned)
	diags.Append(d...)
	if diags.HasError() || ccks.CustomDNSSame(body, current) {
		return nil, diags
	}
	if _, err := ccks.WaitClusterActive(ctx, r.client, id, activeTimeout); err != nil {
		diags.AddError("K8s cluster not active for a custom DNS change", err.Error())
		return nil, diags
	}
	got, err := r.client.PutK8sClusterCustomDNS(ctx, id, body)
	if err != nil {
		diags.AddError("Failed to set the custom DNS of the K8s cluster", err.Error())
	}
	return got, diags
}
