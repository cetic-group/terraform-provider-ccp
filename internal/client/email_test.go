// Tests for the hosted-email client (#932).
//
// They pin the three shapes of the API that no schema expresses: the quota is
// written in gigabytes and read back in bytes, a password never comes back, and
// the domain filter is a query parameter, not a path segment.
package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func domainBody(status string, withDetail bool) map[string]any {
	b := map[string]any{
		"id": "d-1", "name": "example.com", "status": status,
		"externally_managed": false, "created_at": "2026-09-01T10:00:00Z",
		"accounts_count": 2, "aliases_count": 1,
	}
	if withDetail {
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
	}
	return b
}

// The list shape carries none of the three detail blocks. Reading them from a
// list response would silently blank the records the customer has to publish
// (CLAUDE.md pitfall #5) — so the client must leave them nil and the resource
// must read the domain back.
func TestListEmailDomains_HasNoDetailBlocks(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/email/domains", Status: http.StatusOK,
			Body: []map[string]any{domainBody("pending_verification", false)}},
	})
	defer srv.Close()

	domains, err := newClient(srv.URL).ListEmailDomains(context.Background())
	if err != nil {
		t.Fatalf("ListEmailDomains: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(domains))
	}
	if domains[0].Verification != nil || domains[0].ClientConfig != nil || domains[0].Records != nil {
		t.Errorf("the list shape must not pretend to carry detail blocks: %+v", domains[0])
	}
}

func TestGetEmailDomain_CarriesRecordsAndTheirObservedState(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/email/domains/d-1", Status: http.StatusOK,
			Body: domainBody("pending_verification", true)},
	})
	defer srv.Close()

	d, err := newClient(srv.URL).GetEmailDomain(context.Background(), "d-1")
	if err != nil {
		t.Fatalf("GetEmailDomain: %v", err)
	}
	if d.Verification == nil || d.Verification.Value != "ccp-verify=abc" {
		t.Fatalf("verification record dropped: %+v", d.Verification)
	}
	if len(d.Records) != 2 {
		t.Fatalf("expected 2 expected records, got %d", len(d.Records))
	}
	mx := d.Records[0]
	if mx.Hostname == nil || *mx.Hostname != "mail.example.com" || mx.Priority == nil || *mx.Priority != 10 {
		t.Errorf("MX must keep its split display fields as a pair: %+v", mx)
	}
	if mx.Value != "10 mail.example.com." {
		t.Errorf("MX value must stay complete, priority included: %q", mx.Value)
	}
	// Orthogonal to `status`: a value can be right and still not publishable.
	if !d.Records[1].ExceedsLookupLimit || d.Records[1].Status != "ok" {
		t.Errorf("exceeds_lookup_limit must survive independently of status: %+v", d.Records[1])
	}
	if d.ClientConfig == nil || d.ClientConfig.Incoming.Port != 993 {
		t.Errorf("client config dropped: %+v", d.ClientConfig)
	}
}

// The quota is WRITTEN in gigabytes. Sending a zero when the caller did not ask
// for one would silently shrink the mailbox to the smallest quota the API
// accepts instead of taking the platform default.
func TestCreateEmailAccount_OmitsUnsetQuota(t *testing.T) {
	created := map[string]any{
		"id": "a-1", "address": "contact@example.com", "quota_bytes": 1073741824,
		"enabled": true, "enable_imap": true, "enable_pop": false,
		"created_at": "2026-09-01T10:00:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/email/accounts", Status: http.StatusCreated, Body: created},
	})
	defer srv.Close()

	if _, err := newClient(srv.URL).CreateEmailAccount(context.Background(), client.EmailAccountCreateRequest{
		Address: "contact@example.com", Password: "correct horse battery",
	}); err != nil {
		t.Fatalf("CreateEmailAccount: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(srv.Calls()[0].Body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if _, present := sent["quota_gb"]; present {
		t.Errorf("quota_gb sent although unset: %s", srv.Calls()[0].Body)
	}
	if sent["password"] != "correct horse battery" {
		t.Errorf("password not forwarded: %s", srv.Calls()[0].Body)
	}
}

// No response of the email API carries a password, in any shape. A struct field
// for one would be a place for it to end up in state, in a log, or in a diff.
func TestEmailAccount_HasNoPasswordField(t *testing.T) {
	raw, err := json.Marshal(client.EmailAccount{ID: "a-1", Address: "contact@example.com"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var shape map[string]any
	_ = json.Unmarshal(raw, &shape)
	for _, forbidden := range []string{"password", "hash", "secret"} {
		if _, present := shape[forbidden]; present {
			t.Errorf("EmailAccount exposes %q: %s", forbidden, raw)
		}
	}
}

// Resetting a password answers 204: there is nothing to return, and the client
// must not choke on an empty body.
func TestResetEmailAccountPassword_204(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/email/accounts/a-1/password", Status: http.StatusNoContent},
	})
	defer srv.Close()

	if err := newClient(srv.URL).ResetEmailAccountPassword(context.Background(), "a-1", "a new secret"); err != nil {
		t.Fatalf("ResetEmailAccountPassword: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(srv.Calls()[0].Body, &sent)
	if sent["password"] != "a new secret" {
		t.Errorf("password not forwarded: %s", srv.Calls()[0].Body)
	}
}

// The domain filter is a QUERY parameter. Building it into the path would hit a
// route that does not exist, and the 404 would read as "no such domain".
func TestListEmailAccounts_FiltersByQueryNotByPath(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/email/accounts", Status: http.StatusOK, Body: []map[string]any{}},
		{Method: "GET", Path: "/v1/email/accounts", Status: http.StatusOK, Body: []map[string]any{}},
	})
	defer srv.Close()
	c := newClient(srv.URL)

	if _, err := c.ListEmailAccounts(context.Background(), "d-1"); err != nil {
		t.Fatalf("ListEmailAccounts(filtered): %v", err)
	}
	if _, err := c.ListEmailAccounts(context.Background(), ""); err != nil {
		t.Fatalf("ListEmailAccounts(all): %v", err)
	}
	for _, call := range srv.Calls() {
		if call.Path != "/v1/email/accounts" {
			t.Errorf("the domain filter leaked into the path: %s", call.Path)
		}
	}
}

// The alias destinations are read and written whole, and their order is kept —
// which is why the schema models them as a list, not a set.
func TestUpdateEmailAlias_SendsDestinationsWhole(t *testing.T) {
	updated := map[string]any{
		"id": "al-1", "address": "team@example.com",
		"destinations": []string{"alice@example.com", "bob@example.com"},
		"wildcard":     false, "created_at": "2026-09-01T10:00:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/email/aliases/al-1", Status: http.StatusOK, Body: updated},
	})
	defer srv.Close()

	dests := []string{"alice@example.com", "bob@example.com"}
	got, err := newClient(srv.URL).UpdateEmailAlias(context.Background(), "al-1",
		client.EmailAliasUpdateRequest{Destinations: &dests})
	if err != nil {
		t.Fatalf("UpdateEmailAlias: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(srv.Calls()[0].Body, &sent)
	values, _ := sent["destinations"].([]any)
	if len(values) != 2 || values[0] != "alice@example.com" || values[1] != "bob@example.com" {
		t.Errorf("destinations must be sent whole and in order: %s", srv.Calls()[0].Body)
	}
	if len(got.Destinations) != 2 || got.Destinations[0] != "alice@example.com" {
		t.Errorf("order not preserved on read back: %+v", got.Destinations)
	}
}

// Deleting a domain that still carries mailboxes is refused, and the message
// says so. The provider relays it, so the 409 must stay recognisable.
func TestDeleteEmailDomain_ConflictIsRecognisable(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/email/domains/d-1", Status: http.StatusConflict,
			Body: map[string]any{"detail": "Le domaine porte encore des adresses."}},
	})
	defer srv.Close()

	err := newClient(srv.URL).DeleteEmailDomain(context.Background(), "d-1")
	if !client.IsConflict(err) {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if !contains(err.Error(), "porte encore des adresses") {
		t.Errorf("the API message must survive: %v", err)
	}
}
