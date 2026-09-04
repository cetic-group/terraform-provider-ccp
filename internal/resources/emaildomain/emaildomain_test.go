// Tests for ccp_email_domain.
//
// This resource carries the records the customer has to publish to receive mail
// at all, and it claims a name PLATFORM-WIDE the moment it is created. Two
// behaviours matter more than the mapping itself, and both are run for real
// here against a mocked API:
//
//   - a domain still waiting for its proof of ownership is a WARNING, not a
//     failure — failing would throw away the records to publish;
//   - the state is written even when activation fails, because the name is
//     taken from the POST onwards. Returning without it would leave a domain
//     nothing references, that the next apply could neither create (409) nor
//     destroy.
package emaildomain

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func domainSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&emailDomainResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func object(t *testing.T, s schema.Schema, set map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	attrs := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, at := range objType.AttributeTypes {
		if v, ok := set[name]; ok {
			attrs[name] = v
		} else {
			attrs[name] = tftypes.NewValue(at, nil)
		}
	}
	for name := range set {
		if _, ok := objType.AttributeTypes[name]; !ok {
			t.Fatalf("no attribute %q in the compiled schema", name)
		}
	}
	return tftypes.NewValue(objType, attrs)
}

func str(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

func listBody(status string) map[string]any {
	return map[string]any{
		"id": "d-1", "name": "example.com", "status": status,
		"externally_managed": false, "created_at": "2026-09-01T10:00:00Z",
	}
}

func detailBody(status string) map[string]any {
	b := listBody(status)
	b["accounts_count"] = 0
	b["aliases_count"] = 0
	b["verification"] = map[string]any{
		"type": "TXT", "name": "_ccp-verification.example.com",
		"value": "ccp-verify=abc", "status": "missing",
		"purpose": "Prouve que le domaine vous appartient.",
	}
	b["records"] = []map[string]any{
		{"type": "MX", "name": "example.com", "value": "10 mail.example.com.",
			"hostname": "mail.example.com", "priority": 10, "status": "missing",
			"purpose": "Où livrer le courrier entrant."},
		{"type": "TXT", "name": "example.com", "value": "v=spf1 -all", "status": "ok",
			"exceeds_lookup_limit": true, "purpose": "Autorise nos serveurs."},
	}
	b["client_config"] = map[string]any{
		"incoming":      map[string]any{"protocol": "imap", "hostname": "mail.example.com", "port": 993, "security": "tls"},
		"outgoing":      map[string]any{"protocol": "smtp", "hostname": "mail.example.com", "port": 465, "security": "tls"},
		"username_hint": "Utilisez l'adresse complète.",
	}
	return b
}

func runCreate(t *testing.T, url string, s schema.Schema, planned tftypes.Value) *resource.CreateResponse {
	t.Helper()
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	r := &emailDomainResource{client: client.New(url, "ccp_test_unit", "0.0.0-test")}
	resp := &resource.CreateResponse{State: tfsdk.State{Raw: tftypes.NewValue(objType, nil), Schema: s}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Raw: planned, Schema: s}}, resp)
	return resp
}

// A domain on hold must not fail the apply: the records to publish are the whole
// point of that apply, and failing would leave them nowhere.
func TestCreate_PendingIsAWarningAndTheRecordsLandInState(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/email/domains", Status: http.StatusCreated, Body: listBody("pending_verification")},
		{Method: "GET", Path: "/v1/email/domains/d-1", Status: http.StatusOK, Body: detailBody("pending_verification")},
	})
	defer srv.Close()

	s := domainSchema(t)
	planned := object(t, s, map[string]tftypes.Value{
		"name": str("example.com"), "wait_for_verification": tftypes.NewValue(tftypes.Bool, false),
	})
	resp := runCreate(t, srv.URL, s, planned)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a domain waiting for its proof must not fail the apply: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Error("the operator must be told there is a record to publish, and that a mailbox will fail until then")
	}
	if w := resp.Diagnostics.Warnings()[0].Detail(); !strings.Contains(w, "wait_for_verification") {
		t.Errorf("the warning must name the next gesture, got: %s", w)
	}

	var got emailDomainModel
	resp.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "d-1" {
		t.Fatalf("state not written: %+v", got)
	}
	if got.Verification.IsNull() {
		t.Fatal("the record that blocks activation is missing from state — the apply produced nothing usable")
	}
	if v := got.Verification.Attributes()["value"].(types.String).ValueString(); v != "ccp-verify=abc" {
		t.Errorf("verification value lost: %q", v)
	}
	if got.DNSRecords.IsNull() || len(got.DNSRecords.Elements()) != 2 {
		t.Errorf("the MX/SPF records to publish are missing from state: %v", got.DNSRecords)
	}
}

// The name is claimed platform-wide from the POST onwards. An apply that fails
// to activate must STILL record the domain, or it becomes unreachable: the next
// apply can neither create it (409 on the name) nor destroy it.
func TestCreate_StateIsWrittenEvenWhenActivationFails(t *testing.T) {
	prevTimeout, prevInterval := verifyTimeout, verifyInterval
	verifyTimeout, verifyInterval = time.Nanosecond, time.Millisecond
	defer func() { verifyTimeout, verifyInterval = prevTimeout, prevInterval }()

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/email/domains", Status: http.StatusCreated, Body: listBody("pending_verification")},
		// The record is not visible yet, so the platform keeps the domain on hold.
		{Method: "POST", Path: "/v1/email/domains/d-1/verify", Status: http.StatusOK, Body: listBody("pending_verification")},
		{Method: "GET", Path: "/v1/email/domains/d-1", Status: http.StatusOK, Body: detailBody("pending_verification")},
	})
	defer srv.Close()

	s := domainSchema(t)
	planned := object(t, s, map[string]tftypes.Value{
		"name": str("example.com"), "wait_for_verification": tftypes.NewValue(tftypes.Bool, true),
	})
	resp := runCreate(t, srv.URL, s, planned)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a verification that never succeeds must be reported as an error")
	}
	var got emailDomainModel
	resp.State.Get(context.Background(), &got)
	if got.ID.ValueString() != "d-1" {
		t.Fatal("the domain exists and its name is taken, but nothing was recorded — " +
			"the next apply can neither create it (409) nor destroy it")
	}
	if got.Verification.IsNull() {
		t.Error("the record to publish must be in state so the operator can act on the failure")
	}
}

// ─── Mapping ─────────────────────────────────────────────────────────────────

// The list shape carries none of the detail blocks. Writing zeroes over them
// would claim the customer has nothing to publish (CLAUDE.md pitfall #5).
func TestStateFrom_ListShapeLeavesTheDetailBlocksNull(t *testing.T) {
	m, diags := stateFrom(&client.EmailDomain{
		ID: "d-1", Name: "example.com", Status: client.EmailDomainStatusPendingVerification,
		CreatedAt: "2026-09-01T10:00:00Z",
	}, types.BoolValue(false))
	if diags.HasError() {
		t.Fatalf("stateFrom: %v", diags)
	}
	if !m.Verification.IsNull() || !m.DNSRecords.IsNull() || !m.ClientConfig.IsNull() {
		t.Error("absent detail blocks must stay null, not become empty objects")
	}
	if !m.VerifiedAt.IsNull() || !m.AccountsCount.IsNull() {
		t.Error("absent optional fields must stay null, not become zeroes")
	}
}

func TestStateFrom_MXKeepsItsSplitFieldsAsAPair(t *testing.T) {
	host := "mail.example.com"
	prio := int64(10)
	m, diags := stateFrom(&client.EmailDomain{
		ID: "d-1", Name: "example.com", Status: client.EmailDomainStatusActive,
		CreatedAt: "2026-09-01T10:00:00Z",
		Records: []client.EmailDomainDNSRecord{
			{Type: "MX", Name: "example.com", Value: "10 mail.example.com.",
				Hostname: &host, Priority: &prio, Status: "missing", Purpose: "…"},
			{Type: "TXT", Name: "example.com", Value: "v=spf1 -all",
				Status: "ok", ExceedsLookupLimit: true, Purpose: "…"},
		},
	}, types.BoolValue(false))
	if diags.HasError() {
		t.Fatalf("stateFrom: %v", diags)
	}
	mx := m.DNSRecords.Elements()[0].(types.Object).Attributes()
	if mx["hostname"].(types.String).ValueString() != host {
		t.Error("the MX server is dropped — control panels ask for it separately, " +
			"and pasting the whole line into their server field breaks delivery")
	}
	if mx["priority"].(types.Int64).ValueInt64() != prio {
		t.Error("the MX priority is dropped; it always travels with the hostname")
	}
	if mx["value"].(types.String).ValueString() != "10 mail.example.com." {
		t.Error("the canonical MX line must stay complete, priority included")
	}
	txt := m.DNSRecords.Elements()[1].(types.Object).Attributes()
	// Orthogonal to status: the value can be right and still not publishable.
	if !txt["exceeds_lookup_limit"].(types.Bool).ValueBool() || txt["status"].(types.String).ValueString() != "ok" {
		t.Error("exceeds_lookup_limit must survive independently of status")
	}
	// Every other type has neither, and never one without the other.
	if !txt["hostname"].(types.String).IsNull() || !txt["priority"].(types.Int64).IsNull() {
		t.Error("hostname and priority are an MX-only pair: both null elsewhere")
	}
}

// ─── Schema guarantees ───────────────────────────────────────────────────────

func TestNameForcesReplacementAndExternallyManagedIsReadOnly(t *testing.T) {
	s := domainSchema(t)
	name, ok := s.Attributes["name"].(schema.StringAttribute)
	if !ok {
		t.Fatal("name missing")
	}
	if len(name.PlanModifiers) == 0 {
		t.Error("name must force replacement: a mail domain has a single owner and its name is claimed platform-wide")
	}
	ext, ok := s.Attributes["externally_managed"]
	if !ok {
		t.Fatal("externally_managed missing")
	}
	if ext.IsOptional() || ext.IsRequired() {
		t.Error("externally_managed is set by a platform administration route, never by this " +
			"resource: offering it would let a plan claim a control plane it cannot take")
	}
}
