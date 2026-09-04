// Tests for ccp_dns_zone.
//
// The mapping is where this resource can go wrong quietly: the API answers with
// the level actually SERVING the network (`resolver.tier`), which is not always
// the level that was ASKED for. Writing one over the other loses information the
// operator needs, or aborts the apply.
package dnszone

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func servedZone(status, resolverTier string) *client.DNSZone {
	cidr := "10.20.0.0/24"
	count := int64(3)
	return &client.DNSZone{
		ID: "z-1", Name: "corp.internal", VpcID: "vpc-1", Region: "RNN",
		Status: status, DefaultTTL: 300, DNSSECEnabled: false,
		CreatedAt: "2026-09-01T10:00:00Z", RecordSetsCount: &count,
		Resolver: &client.DNSResolverInfo{
			Addresses: []string{"10.20.0.51", "10.21.0.51"},
			Endpoints: []client.DNSResolverEndpoint{
				{Address: "10.20.0.51", VnetID: "vn-1", VnetName: "office", VnetCIDR: &cidr},
				{Address: "10.21.0.51", VnetID: "vn-2", VnetName: ""},
			},
			Tier: resolverTier, Status: "active",
			NsHostname: "ns1.dns.example", AppliesToNewGuestsOnly: true,
		},
	}
}

func TestStateFrom_MapsEveryResolverEndpointWithItsSubnet(t *testing.T) {
	m, diags := stateFrom(context.Background(), servedZone("active", "prod"), types.BoolValue(false))
	if diags.HasError() {
		t.Fatalf("stateFrom: %v", diags)
	}
	if len(m.ResolverAddresses.Elements()) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(m.ResolverAddresses.Elements()))
	}
	if len(m.ResolverEndpoints.Elements()) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(m.ResolverEndpoints.Elements()))
	}
	if m.ResolverStatus.ValueString() != "active" || m.NsHostname.ValueString() != "ns1.dns.example" {
		t.Errorf("resolver fields dropped: %+v", m)
	}
	if !m.AppliesToNewGuestsOnly.ValueBool() {
		t.Error("applies_to_new_guests_only must be reported: it is what tells the operator why an existing machine does not see the zone")
	}
	// An empty subnet name is DATA, not a hole to fill with a placeholder.
	second := m.ResolverEndpoints.Elements()[1].(types.Object).Attributes()
	if second["vnet_name"].(types.String).ValueString() != "" {
		t.Error("an empty subnet name must be reported as it is")
	}
	if !second["vnet_cidr"].(types.String).IsNull() {
		t.Error("a missing CIDR must stay null, not become an empty string")
	}
}

// `tier` is what the operator asked for; `resolver_tier` is what is serving the
// network. Overwriting the first with the second is CLAUDE.md pitfall #1: the
// planned value would differ from the applied one and Terraform would abort.
func TestApplyTier_KeepsWhatWasConfigured(t *testing.T) {
	// A zone still waiting for its proof reports the REQUESTED level, and a
	// served network can report a different one than the last request.
	m, _ := stateFrom(context.Background(), servedZone("pending_verification", "dev"), types.BoolValue(false))
	applyTier(&m, types.StringValue("prod"))

	if m.Tier.ValueString() != "prod" {
		t.Errorf("tier = %q, want the configured value", m.Tier.ValueString())
	}
	if m.ResolverTier.ValueString() != "dev" {
		t.Errorf("resolver_tier = %q, want the level actually serving", m.ResolverTier.ValueString())
	}
}

// On import nothing was configured, so the API's answer is all there is.
func TestApplyTier_FallsBackToTheServedLevel(t *testing.T) {
	m, _ := stateFrom(context.Background(), servedZone("active", "prod"), types.BoolNull())
	applyTier(&m, types.StringNull())
	if m.Tier.ValueString() != "prod" {
		t.Errorf("tier = %q, want the served level on import", m.Tier.ValueString())
	}
	if m.WaitForVerification.ValueBool() {
		t.Error("wait_for_verification must default to false, never be left unknown")
	}
}

// The list shape has no resolver block. Mapping zero values over it would claim
// the name server has no address at all (CLAUDE.md pitfall #5).
func TestStateFrom_AbsentResolverStaysNull(t *testing.T) {
	z := servedZone("active", "prod")
	z.Resolver = nil
	m, diags := stateFrom(context.Background(), z, types.BoolValue(false))
	if diags.HasError() {
		t.Fatalf("stateFrom: %v", diags)
	}
	if !m.ResolverAddresses.IsNull() || !m.ResolverEndpoints.IsNull() {
		t.Error("an absent resolver block must stay null, not become an empty list")
	}
	if !m.ResolverStatus.IsNull() || !m.NsHostname.IsNull() {
		t.Error("absent resolver fields must stay null, not become empty strings")
	}
}

// The ownership challenge exists only while a public domain name is on hold. It
// is the only thing that says what to publish, so it must survive the mapping —
// and it must be null when there is nothing to prove.
func TestStateFrom_OwnershipChallenge(t *testing.T) {
	m, _ := stateFrom(context.Background(), servedZone("active", "dev"), types.BoolValue(false))
	if !m.OwnershipChallenge.IsNull() {
		t.Error("an internal suffix has nothing to prove: the challenge must be null")
	}

	z := servedZone("pending_verification", "dev")
	z.OwnershipChallenge = &client.DNSOwnershipChallenge{
		RecordName: "_ccp-dns-verification.example.com", RecordType: "TXT", RecordValue: "ccp=abc",
	}
	m, _ = stateFrom(context.Background(), z, types.BoolValue(false))
	if m.OwnershipChallenge.IsNull() {
		t.Fatal("challenge dropped — the operator would have nothing to publish")
	}
	got := m.OwnershipChallenge.Attributes()
	if got["record_value"].(types.String).ValueString() != "ccp=abc" {
		t.Errorf("challenge value lost: %+v", got)
	}
}
