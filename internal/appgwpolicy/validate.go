package appgwpolicy

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type PolicyValidator struct{ Kind string }

func (v PolicyValidator) Description(context.Context) string {
	return "A bounded, unambiguous Application Gateway access policy."
}
func (v PolicyValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func fullyKnown(value attr.Value) bool {
	if value.IsUnknown() {
		return false
	}
	if value.IsNull() {
		return true
	}
	switch v := value.(type) {
	case types.Object:
		for _, x := range v.Attributes() {
			if !fullyKnown(x) {
				return false
			}
		}
	case types.Map:
		for _, x := range v.Elements() {
			if !fullyKnown(x) {
				return false
			}
		}
	case types.List:
		for _, x := range v.Elements() {
			if !fullyKnown(x) {
				return false
			}
		}
	}
	return true
}
func (v PolicyValidator) ValidateObject(ctx context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || !fullyKnown(req.ConfigValue) {
		return
	}
	var err error
	switch v.Kind {
	case "l7":
		var p *client.AppGWL7Policy
		p, err = L7(req.ConfigValue)
		if err == nil {
			err = ValidateL7(p)
		}
	case "l4":
		_, err = L4(req.ConfigValue)
	case "client_ip":
		_, err = ClientIP(req.ConfigValue)
	}
	if err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Application Gateway access policy", err.Error())
	}
}
func ValidateL4(p *client.AppGWL4Policy) error {
	for _, tcp := range []*client.AppGWTCPPolicy{p.TCP80, p.TCP443} {
		if tcp != nil {
			if err := validateCIDRs(tcp.AllowCIDRs); err != nil {
				return err
			}
			if err := validateCIDRs(tcp.DenyCIDRs); err != nil {
				return err
			}
		}
	}
	return nil
}
func ValidateClientIP(p *client.AppGWClientIPPolicy) error {
	if p.Mode != "" && p.Mode != "peer" && p.Mode != "trusted_xff" {
		return fmt.Errorf("client IP mode must be peer or trusted_xff")
	}
	if err := validateCIDRs(p.TrustedProxyCIDRs); err != nil {
		return err
	}
	if (p.Mode == "trusted_xff") == (len(p.TrustedProxyCIDRs) == 0) {
		return fmt.Errorf("trusted_xff requires trusted networks, and peer mode does not accept them")
	}
	return nil
}
func validateCIDRs(values []string) error {
	if len(values) > 256 {
		return fmt.Errorf("an address list may contain at most 256 entries")
	}
	for _, value := range values {
		if len(value) > 64 || strings.TrimSpace(value) != value || strings.Contains(value, "%") {
			return fmt.Errorf("address lists require unambiguous IPv4/IPv6 addresses or CIDR prefixes")
		}
		if strings.Contains(value, "/") {
			if _, err := netip.ParsePrefix(value); err != nil {
				return fmt.Errorf("invalid address prefix in access policy")
			}
		} else if _, err := netip.ParseAddr(value); err != nil {
			return fmt.Errorf("invalid address in access policy")
		}
	}
	return nil
}

var headerToken = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,64}$")
var methodToken = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,31}$`)
var keyToken = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

func validateHeader(h client.AppGWSecurityHeader) error {
	if !headerToken.MatchString(h.Name) {
		return fmt.Errorf("header names must be HTTP tokens of at most 64 characters")
	}
	if len(h.Values) > 16 {
		return fmt.Errorf("a header condition supports at most 16 values")
	}
	for _, value := range h.Values {
		if len(value) < 1 || len(value) > 256 || strings.Trim(value, " \t") != value {
			return fmt.Errorf("header values must be non-empty ASCII scalars of at most 256 bytes")
		}
		for _, b := range []byte(value) {
			if b < 32 || b > 126 || b == ',' {
				return fmt.Errorf("header values must be visible ASCII scalars without commas")
			}
		}
	}
	switch h.Op {
	case "eq":
		if len(h.Values) != 1 {
			return fmt.Errorf("eq requires exactly one header value")
		}
	case "in":
		if len(h.Values) == 0 {
			return fmt.Errorf("in requires header values")
		}
	case "present", "absent", "non_empty":
		if len(h.Values) > 0 {
			return fmt.Errorf("presence operations do not accept values")
		}
	default:
		return fmt.Errorf("unknown header operation")
	}
	return nil
}
func validatePath(p *client.AppGWSecurityPath) error {
	if p == nil {
		return nil
	}
	if p.Op != "exact" && p.Op != "segment_prefix" {
		return fmt.Errorf("invalid path operation")
	}
	_, err := CanonicalPath(p.Value)
	return err
}
func ValidateL7(p *client.AppGWL7Policy) error {
	if p == nil {
		return nil
	}
	if len(p.Policies) > 8 {
		return fmt.Errorf("at most eight policies can be attached to a resource")
	}
	total := 0
	for _, policy := range p.Policies {
		if !keyToken.MatchString(policy.Key) {
			return fmt.Errorf("invalid policy identifier")
		}
		if policy.Scope != nil {
			if err := validatePath(policy.Scope.Path); err != nil {
				return err
			}
		}
		keys := map[string]bool{}
		addKey := func(key string) error {
			if !keyToken.MatchString(key) || keys[key] {
				return fmt.Errorf("rule and requirement identifiers must be valid and unique within their policy")
			}
			keys[key] = true
			total++
			return nil
		}
		for _, header := range policy.RequiredHeaders {
			if err := addKey(header.Key); err != nil {
				return err
			}
			if err := validateHeader(header); err != nil {
				return err
			}
		}
		for _, rules := range [][]client.AppGWSecurityRule{policy.AllowRules, policy.DenyRules} {
			for _, rule := range rules {
				if err := addKey(rule.Key); err != nil {
					return err
				}
				m := rule.Match
				criteria := m.SourceCIDRs != nil || m.Path != nil || m.Methods != nil || m.Headers != nil
				if (m.MatchAll != nil && *m.MatchAll) == criteria || (m.MatchAll != nil && !*m.MatchAll) {
					return fmt.Errorf("a rule requires criteria or match_all=true, exclusively")
				}
				if m.SourceCIDRs != nil && len(m.SourceCIDRs) == 0 {
					return fmt.Errorf("specified criteria must not be empty")
				}
				if err := validateCIDRs(m.SourceCIDRs); err != nil {
					return err
				}
				if err := validatePath(m.Path); err != nil {
					return err
				}
				if m.Methods != nil && (len(m.Methods) == 0 || len(m.Methods) > 16) {
					return fmt.Errorf("method criteria require one to sixteen methods")
				}
				for _, method := range m.Methods {
					if !methodToken.MatchString(method) {
						return fmt.Errorf("methods must be uppercase HTTP tokens")
					}
				}
				if m.Headers != nil && (len(m.Headers) == 0 || len(m.Headers) > 8) {
					return fmt.Errorf("header criteria require one to eight conditions")
				}
				for _, header := range m.Headers {
					if err := validateHeader(header); err != nil {
						return err
					}
				}
			}
		}
		if len(keys) == 0 {
			return fmt.Errorf("each policy requires at least one rule or header requirement")
		}
		if len(policy.RequiredHeaders) > 8 {
			return fmt.Errorf("at most eight header requirements per policy")
		}
	}
	if total > 64 {
		return fmt.Errorf("at most 64 rules and requirements are supported per gateway, including its children")
	}
	return nil
}

// CanonicalPath mirrors the public path contract; ambiguous delimiters are
// rejected rather than rewritten into a different backend request.
func CanonicalPath(value string) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("path contains an ambiguous encoding, delimiter or segment")
	}
	raw := []byte(value)
	if len(raw) == 0 || raw[0] != '/' || len(raw) > 4096 {
		return invalid()
	}
	decoded := []byte{}
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		encoded := b == '%'
		if encoded {
			if i+2 >= len(raw) {
				return invalid()
			}
			parsed, err := strconv.ParseUint(string(raw[i+1:i+3]), 16, 8)
			if err != nil {
				return invalid()
			}
			b = byte(parsed)
			i += 2
		}
		if b < 32 || b == 127 || strings.ContainsRune(`\;?#`, rune(b)) || (encoded && (b == '/' || b == '%')) {
			return invalid()
		}
		decoded = append(decoded, b)
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~/", rune(b)) {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	if !utf8.Valid(decoded) {
		return invalid()
	}
	result := out.String()
	if strings.Contains(result, "//") {
		return invalid()
	}
	for _, part := range strings.Split(result, "/") {
		if part == "." || part == ".." {
			return invalid()
		}
	}
	return result, nil
}
