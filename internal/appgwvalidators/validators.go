// Package appgwvalidators provides shared validators for Application
// Gateway resources — hostname (RFC 1123), CIDR (IPv4/IPv6 with mask),
// algorithm/path-match-type/WAF-preset enums.
//
// We use the framework's `validator.String` / `validator.List` interfaces;
// each validator silently passes when the value is null or unknown so it
// composes safely with plan-time defaults (cf. ValidateConfig pitfall #4
// in CLAUDE.md).
package appgwvalidators

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// hostnameRegex matches a DNS-1123 hostname. Labels are 1-63 chars,
// alphanumeric with hyphens (no leading/trailing hyphen), separated by
// dots. Total length is checked separately (max 253).
var hostnameRegex = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// hostnameValidator enforces RFC 1123 hostnames.
type hostnameValidator struct{}

func (hostnameValidator) Description(_ context.Context) string {
	return "must be a valid RFC 1123 hostname (max 253 chars, labels 1-63 chars)"
}
func (v hostnameValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (hostnameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if len(s) == 0 || len(s) > 253 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid hostname",
			fmt.Sprintf("hostname must be 1-253 chars, got %d", len(s)))
		return
	}
	if !hostnameRegex.MatchString(s) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid hostname",
			"must be a valid RFC 1123 hostname (e.g. `api.example.com`)")
	}
}

// Hostname returns a validator that enforces RFC 1123 hostnames.
func Hostname() validator.String { return hostnameValidator{} }

// cidrValidator enforces a single IPv4 or IPv6 CIDR string.
type cidrValidator struct{}

func (cidrValidator) Description(_ context.Context) string {
	return "must be a valid IPv4 or IPv6 CIDR (e.g. `10.0.0.0/24`, `2001:db8::/32`)"
}
func (v cidrValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (cidrValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if _, _, err := net.ParseCIDR(s); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR",
			fmt.Sprintf("%q is not a valid CIDR: %v", s, err))
	}
}

// CIDR returns a validator that enforces a valid IPv4/IPv6 CIDR.
func CIDR() validator.String { return cidrValidator{} }

// cidrListValidator enforces every element of a List(String) is a valid CIDR.
type cidrListValidator struct{}

func (cidrListValidator) Description(_ context.Context) string {
	return "every element must be a valid IPv4 or IPv6 CIDR"
}
func (v cidrListValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (cidrListValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var elems []types.String
	d := req.ConfigValue.ElementsAs(ctx, &elems, false)
	if d.HasError() {
		// Element type mismatch — surface the framework's own error rather
		// than masking it.
		resp.Diagnostics.Append(d...)
		return
	}
	for i, e := range elems {
		if e.IsNull() || e.IsUnknown() {
			continue
		}
		s := e.ValueString()
		if strings.TrimSpace(s) == "" {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR",
				fmt.Sprintf("element %d is empty", i))
			continue
		}
		if _, _, err := net.ParseCIDR(s); err != nil {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR",
				fmt.Sprintf("element %d (%q) is not a valid CIDR: %v", i, s, err))
		}
	}
}

// CIDRList returns a validator that enforces every list element is a
// valid IPv4/IPv6 CIDR.
func CIDRList() validator.List { return cidrListValidator{} }

// ─── Constant enums (re-exported for code reuse in schemas) ─────────────────

// Algorithms accepted on target groups.
var Algorithms = []string{"roundrobin", "leastconn", "source"}

// PathMatchTypes accepted on route path_match_type.
var PathMatchTypes = []string{"prefix", "exact", "regex"}

// WAFPresets accepted on route waf_preset.
var WAFPresets = []string{"off", "permissive", "strict"}

// HCProtocols accepted on target group hc_protocol.
var HCProtocols = []string{"http", "https", "tcp"}

// HCMethods accepted on target group hc_method (HTTP verbs).
var HCMethods = []string{"GET", "HEAD", "POST", "OPTIONS"}

// NOTE: the gateway `plan` attribute is intentionally NOT validated client-side.
// Plan keys are dynamic (DB-driven `compute_plans`, kind='appgw') and validated
// by the API at create/update time — a hardcoded list here would require a
// provider release for every new backoffice plan (cf. v4.1.1).

// HTTPMethods accepted on route method_match (full HTTP verb set).
var HTTPMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE"}

// HeaderOps accepted on route header_matches[].op.
var HeaderOps = []string{"eq", "prefix", "regex"}
