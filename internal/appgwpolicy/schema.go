// Package appgwpolicy shares typed policy attributes across gateway resources.
package appgwpolicy

import (
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const Ownership = " Omit to preserve remote protection, including after import. Set an explicit empty object `{}` to clear it. Removing this attribute relinquishes management; it does not clear the remote policy. Requires platform access-policy capability version 1."

func stringsList(description string, max int) schema.ListAttribute {
	return schema.ListAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: description, Validators: []validator.List{listvalidator.SizeAtMost(max)}}
}
func pathAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "A canonical security path evaluated before rewrites.", Attributes: map[string]schema.Attribute{
		"op":    schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("exact", "segment_prefix")}, MarkdownDescription: "Exact path or path and subpaths; `/admin` does not match `/administrator`."},
		"value": schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 4096)}, MarkdownDescription: "Path beginning with `/`. Ambiguous encodings and segments are rejected by the platform."},
	}}
}
func headerAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name":   schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.LengthBetween(1, 64)}, MarkdownDescription: "HTTP header name, case-insensitive."},
		"op":     schema.StringAttribute{Required: true, Validators: []validator.String{stringvalidator.OneOf("present", "absent", "non_empty", "eq", "in")}, MarkdownDescription: "Header requirement: present, absent, non_empty, eq or in."},
		"values": stringsList("Exact case-sensitive visible ASCII values. `eq` requires one value; `in` requires at least one. Other operations accept no values. Values may not contain commas.", 16),
	}
}
func keyed(attributes map[string]schema.Attribute, max int, description string) schema.MapNestedAttribute {
	return schema.MapNestedAttribute{
		Optional: true, MarkdownDescription: description, NestedObject: schema.NestedAttributeObject{Attributes: attributes}, Validators: []validator.Map{
			mapvalidator.SizeAtMost(max), mapvalidator.KeysAre(stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`), "Keys start with a lowercase letter and contain at most 32 lowercase letters, digits, hyphens or underscores.")),
		}}
}
func rules() schema.MapNestedAttribute {
	return keyed(map[string]schema.Attribute{
		"match": schema.SingleNestedAttribute{Required: true, MarkdownDescription: "All specified criteria must match (AND). Use only match_all=true for an unconditional rule within its policy scope.", Attributes: map[string]schema.Attribute{
			"source_cidrs": stringsList("Client IPv4/IPv6 addresses or networks. A specified list must not be empty.", 256),
			"path":         pathAttribute(), "methods": stringsList("Uppercase HTTP method tokens. A specified list must not be empty.", 16),
			"headers":   schema.ListNestedAttribute{Optional: true, MarkdownDescription: "Header criteria combined with AND; repeated or comma-separated examined fields are rejected.", NestedObject: schema.NestedAttributeObject{Attributes: headerAttributes()}, Validators: []validator.List{listvalidator.SizeBetween(1, 8)}},
			"match_all": schema.BoolAttribute{Optional: true, MarkdownDescription: "Explicitly match every request within the policy scope; only true is accepted, with no other criterion."},
		}},
	}, 64, "Rules keyed by stable identifiers, combined with OR. Rule and requirement identifiers must be unique within each policy.")
}
func L7Schema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true, Computed: true, MarkdownDescription: "HTTP access policies. Applicable gateway, listener and route policies intersect; any deny wins, and header requirements remain mandatory. A local allow never bypasses authentication or inherited protections." + Ownership,
		PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{Optional: true, MarkdownDescription: "Enable these policies; false when omitted."},
			"policies": keyed(map[string]schema.Attribute{
				"scope":       schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "Protected request scope, independent of allow rules and route selection.", Attributes: map[string]schema.Attribute{"path": pathAttribute()}},
				"allow_rules": rules(), "deny_rules": rules(),
				"required_headers": keyed(headerAttributes(), 8, "Mandatory header conditions, combined with AND independently of allow alternatives."),
			}, 8, "Policies keyed by stable identifiers. All policies whose scope matches must be satisfied."),
		}, Validators: []validator.Object{PolicyValidator{Kind: "l7"}},
	}
}
func tcpAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{Optional: true, MarkdownDescription: "Restrictions for this TCP port; deny wins, empty allow means unrestricted.", Attributes: map[string]schema.Attribute{
		"allow_cidrs": stringsList("Allowed source IPv4/IPv6 addresses or networks.", 256), "deny_cidrs": stringsList("Denied source IPv4/IPv6 addresses or networks.", 256),
	}}
}
func L4Schema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{Optional: true, Computed: true, MarkdownDescription: "Filter TCP peers before HTTP and TLS on ports 80 and 443. Restricting port 80 is incompatible with HTTP-01 certificates." + Ownership, PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()}, Attributes: map[string]schema.Attribute{
		"enabled": schema.BoolAttribute{Optional: true, MarkdownDescription: "Enable TCP filtering; false when omitted."}, "tcp_80": tcpAttribute(), "tcp_443": tcpAttribute(),
	}, Validators: []validator.Object{PolicyValidator{Kind: "l4"}}}
}
func ClientIPSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{Optional: true, Computed: true, MarkdownDescription: "Client address interpretation for new HTTP policies only. Legacy IP lists and TCP filtering retain the peer address." + Ownership, PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()}, Attributes: map[string]schema.Attribute{
		"mode":                schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.OneOf("peer", "trusted_xff")}, MarkdownDescription: "peer (default), or trusted_xff with explicit trusted proxy networks."},
		"trusted_proxy_cidrs": stringsList("Trusted proxy networks; required with trusted_xff and empty with peer. Only trust networks you control.", 256),
	}, Validators: []validator.Object{PolicyValidator{Kind: "client_ip"}}}
}
func L7Type() types.ObjectType       { return L7Schema().GetType().(types.ObjectType) }
func L4Type() types.ObjectType       { return L4Schema().GetType().(types.ObjectType) }
func ClientIPType() types.ObjectType { return ClientIPSchema().GetType().(types.ObjectType) }
