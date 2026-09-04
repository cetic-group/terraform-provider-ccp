// Tests for ccp_dns_record.
//
// Two things are pinned here because getting either wrong produces a defect
// that only shows up long after the apply that introduced it:
//
//   - `records` must be a SET. A list shows a change at every plan once the
//     API returns the values in its own order.
//   - the configured `name` must survive the round trip. The API answers with
//     the fully qualified form, and writing that over a Required attribute is
//     what raises "inconsistent result after apply".
package dnsrecord

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func recordSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&dnsRecordResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// A list would propose a change at every `terraform plan` as soon as the API
// returned the values in a different order — with nothing having moved.
func TestRecordsIsASet(t *testing.T) {
	attr, ok := recordSchema(t).Attributes["records"]
	if !ok {
		t.Fatal("records attribute missing")
	}
	if _, isSet := attr.(schema.SetAttribute); !isSet {
		t.Fatalf("records must be a Set, got %T", attr)
	}
	if !attr.IsRequired() {
		t.Error("records must be Required: it is the desired truth, sent whole")
	}
}

// `name` and `type` identify the record set: the API has no route to change
// them, so a plan that tried to would be rejected at apply.
func TestIdentityAttributesForceReplacement(t *testing.T) {
	s := recordSchema(t)
	for _, name := range []string{"zone_id", "name", "type"} {
		attr, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s missing or not a string attribute", name)
		}
		if len(attr.PlanModifiers) == 0 {
			t.Errorf("%s must force replacement — the API cannot change it in place", name)
		}
	}
	ttl, ok := s.Attributes["ttl"].(schema.Int64Attribute)
	if !ok {
		t.Fatal("ttl missing")
	}
	if len(ttl.PlanModifiers) != 0 {
		t.Error("ttl is changed in place; forcing replacement would destroy a record to edit a number")
	}
}

// `NS` is accepted by the API but must not be offered: the one at the apex is
// read only, and anywhere else it is a delegation a private zone refuses.
// Offering it would only produce plans the API rejects.
func TestNSIsNotOffered(t *testing.T) {
	for _, rt := range recordTypes {
		if rt == "NS" {
			t.Fatal("NS must not be an offered record type")
		}
	}
	for _, want := range []string{"A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA"} {
		found := false
		for _, rt := range recordTypes {
			if rt == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s must be offered", want)
		}
	}
}

// qualify reproduces the platform's own name resolution, including the case
// that has no heuristic: a relative name that happens to END with the zone name
// is still relative.
func TestQualify(t *testing.T) {
	for _, tc := range []struct{ name, zone, want string }{
		{"www", "corp.internal", "www.corp.internal"},
		{"www.corp.internal", "corp.internal", "www.corp.internal"},
		{"www.corp.internal.", "corp.internal", "www.corp.internal"},
		{"WWW", "corp.internal", "www.corp.internal"},
		{"@", "corp.internal", "corp.internal"},
		{"", "corp.internal", "corp.internal"},
		{"corp.internal", "corp.internal", "corp.internal"},
		{"_sip._tcp", "corp.internal", "_sip._tcp.corp.internal"},
		// The one that has no heuristic: `corp` is a suffix of the zone name but
		// not the zone itself, so the name is relative.
		{"intranet.corp", "corp.internal", "intranet.corp.corp.internal"},
	} {
		if got := qualify(tc.name, tc.zone); got != tc.want {
			t.Errorf("qualify(%q, %q) = %q, want %q", tc.name, tc.zone, got, tc.want)
		}
	}
}

// The configured name must come back untouched. Writing the API's fully
// qualified form over a Required attribute is what raises "inconsistent result
// after apply" — the canonical form belongs in `fqdn`.
func TestStateFrom_KeepsTheConfiguredName(t *testing.T) {
	rs := &client.DNSRecordSet{
		ID: "r-1", ZoneID: "z-1", Name: "www.corp.internal", RecordType: "A",
		TTL: 300, Records: []string{"10.20.0.10"}, CreatedAt: "2026-09-01T10:00:00Z",
	}
	m, diags := stateFrom(context.Background(), rs, types.StringValue("www"), types.StringValue("A"))
	if diags.HasError() {
		t.Fatalf("stateFrom: %v", diags)
	}
	if m.Name.ValueString() != "www" {
		t.Errorf("configured name overwritten with %q", m.Name.ValueString())
	}
	if m.FQDN.ValueString() != "www.corp.internal" {
		t.Errorf("fqdn = %q, want the canonical form", m.FQDN.ValueString())
	}
}

// On import there is no configured name yet, so the canonical form is the best
// answer available — and the only one.
func TestStateFrom_FallsBackToTheCanonicalNameOnImport(t *testing.T) {
	rs := &client.DNSRecordSet{
		ID: "r-1", ZoneID: "z-1", Name: "www.corp.internal", RecordType: "A",
		TTL: 300, Records: []string{"10.20.0.10"}, CreatedAt: "2026-09-01T10:00:00Z",
	}
	m, _ := stateFrom(context.Background(), rs, types.StringNull(), types.StringNull())
	if m.Name.ValueString() != "www.corp.internal" || m.Type.ValueString() != "A" {
		t.Errorf("import fallback lost: name=%q type=%q", m.Name.ValueString(), m.Type.ValueString())
	}
}

// The platform trims values. A padded one would come back different from the
// configured one, and Terraform would abort the apply far from the cause.
func TestNoSurroundingSpacePattern(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"10.20.0.10", true},
		{`"v=spf1 -all"`, true},
		{"10 mail.corp.internal.", true},
		{" 10.20.0.10", false},
		{"10.20.0.10 ", false},
		{" ", false},
	} {
		if got := noSurroundingSpace.MatchString(tc.value); got != tc.valid {
			t.Errorf("value %q accepted=%v, want %v", tc.value, got, tc.valid)
		}
	}
}
