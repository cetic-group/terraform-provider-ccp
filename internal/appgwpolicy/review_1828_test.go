package appgwpolicy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

func TestRuleBudgetExcludesIndependentHeaderRequirements(t *testing.T) {
	all := true
	rules := make([]client.AppGWSecurityRule, 64)
	for i := range rules {
		rules[i] = client.AppGWSecurityRule{Key: fmt.Sprintf("rule-%d", i), Match: client.AppGWSecurityMatch{MatchAll: &all}}
	}
	policy := &client.AppGWL7Policy{Enabled: true, Policies: []client.AppGWHTTPPolicy{{
		Key: "protected", AllowRules: rules,
		RequiredHeaders: []client.AppGWSecurityHeader{{Key: "partner", Name: "X-Partner", Op: "present"}},
	}}}
	if err := ValidateL7(policy); err != nil {
		t.Fatalf("API accepts 64 rules plus independent header requirements: %v", err)
	}
	policy.Policies[0].AllowRules = append(rules, client.AppGWSecurityRule{Key: "too-many", Match: client.AppGWSecurityMatch{MatchAll: &all}})
	if err := ValidateL7(policy); err == nil {
		t.Fatal("65 rules must exceed the rule budget")
	}
}

func TestCanonicalPathBoundsItsEncodedOutput(t *testing.T) {
	if _, err := CanonicalPath("/" + strings.Repeat("é", 682)); err != nil {
		t.Fatalf("4093-byte canonical path must remain valid: %v", err)
	}
	if _, err := CanonicalPath("/" + strings.Repeat("é", 683)); err == nil {
		t.Fatal("4099-byte canonical path must be rejected before a request")
	}
}
