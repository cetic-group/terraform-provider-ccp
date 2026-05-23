// Tests for ccp_ssh_key — focuses on the v0.20 scope contract:
// `scope` is Optional+Default("user"), and `created_by_tenant_id` is
// Computed and may be empty on legacy rows. Schema-level RequiresReplace
// on `scope` is verified by reading the planmodifiers from the schema.
package sshkey

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// TestCreate_DefaultScopeUser asserts that when no scope is sent in the body,
// the API still responds with the canonical "user" scope and the client
// model captures it verbatim.
func TestCreate_DefaultScopeUser(t *testing.T) {
	body := map[string]any{
		"id":                   "k-user",
		"name":                 "ops",
		"fingerprint":          "SHA256:abc",
		"scope":                "user",
		"created_by_tenant_id": "tnt-1",
		"created_at":           "2026-05-22T10:00:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/ssh-keys", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateSSHKey(context.Background(), client.SSHKeyCreateRequest{
		Name:      "ops",
		PublicKey: "ssh-ed25519 AAAA",
	})
	if err != nil {
		t.Fatalf("CreateSSHKey: %v", err)
	}
	if got.Scope != "user" {
		t.Fatalf("expected scope=user, got %q", got.Scope)
	}
	if got.CreatedByTenantID != "tnt-1" {
		t.Fatalf("expected created_by_tenant_id=tnt-1, got %q", got.CreatedByTenantID)
	}

	// The body must NOT include scope when the caller omits it — the
	// server is then free to apply its default.
	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(calls))
	}
	if strings.Contains(string(calls[0].Body), `"scope"`) {
		t.Errorf("create body should omit scope when caller leaves it empty, got %s", string(calls[0].Body))
	}
}

// TestCreate_ScopeOrg asserts that an explicit "org" scope is forwarded
// in the JSON body verbatim.
func TestCreate_ScopeOrg(t *testing.T) {
	body := map[string]any{
		"id":                   "k-org",
		"name":                 "team-deploy",
		"fingerprint":          "SHA256:def",
		"scope":                "org",
		"created_by_tenant_id": "tnt-1",
		"created_at":           "2026-05-22T10:01:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/ssh-keys", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateSSHKey(context.Background(), client.SSHKeyCreateRequest{
		Name:      "team-deploy",
		PublicKey: "ssh-ed25519 BBBB",
		Scope:     "org",
	})
	if err != nil {
		t.Fatalf("CreateSSHKey(scope=org): %v", err)
	}
	if got.Scope != "org" {
		t.Fatalf("expected scope=org, got %q", got.Scope)
	}

	var sent struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(srv.Calls()[0].Body, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if sent.Scope != "org" {
		t.Errorf("expected request body scope=org, got %q", sent.Scope)
	}
}

// TestCreate_ScopeTenant asserts owner-only "tenant" scope is forwarded
// verbatim too.
func TestCreate_ScopeTenant(t *testing.T) {
	body := map[string]any{
		"id":                   "k-tnt",
		"name":                 "platform-master",
		"fingerprint":          "SHA256:ghi",
		"scope":                "tenant",
		"created_by_tenant_id": "tnt-1",
		"created_at":           "2026-05-22T10:02:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/ssh-keys", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateSSHKey(context.Background(), client.SSHKeyCreateRequest{
		Name:      "platform-master",
		PublicKey: "ssh-ed25519 CCCC",
		Scope:     "tenant",
	})
	if err != nil {
		t.Fatalf("CreateSSHKey(scope=tenant): %v", err)
	}
	if got.Scope != "tenant" {
		t.Fatalf("expected scope=tenant, got %q", got.Scope)
	}
}

// TestList_LegacyKeyMissingScope verifies that older keys returned by the
// API without a `scope` field don't cause the client to barf — Go's JSON
// decoder will leave the field empty, and the resource Read collapses it
// to "user" (verified by inspection of the resource code).
func TestList_LegacyKeyMissingScope(t *testing.T) {
	body := []map[string]any{{
		"id":          "k-legacy",
		"name":        "old-key",
		"fingerprint": "SHA256:legacy",
		// no `scope`, no `created_by_tenant_id`
		"created_at": "2025-01-01T00:00:00Z",
	}}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/ssh-keys", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	keys, err := c.ListSSHKeys(context.Background())
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Scope != "" {
		t.Errorf("legacy row should decode with empty scope (collapsed by resource Read), got %q", keys[0].Scope)
	}
	if keys[0].CreatedByTenantID != "" {
		t.Errorf("legacy row should decode with empty created_by_tenant_id, got %q", keys[0].CreatedByTenantID)
	}
}

// TestSchema_ScopeForceNew asserts the `scope` attribute carries a
// RequiresReplace plan modifier. The CCP backend does not support
// mutating the scope of an existing row, so this is part of the contract.
func TestSchema_ScopeForceNew(t *testing.T) {
	r := New().(*sshKeyResource)
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema build errors: %v", resp.Diagnostics.Errors())
	}
	attr, ok := resp.Schema.Attributes["scope"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("scope attribute missing or wrong type: %T", resp.Schema.Attributes["scope"])
	}
	if !attr.Optional || !attr.Computed {
		t.Errorf("scope should be Optional+Computed, got optional=%v computed=%v", attr.Optional, attr.Computed)
	}
	if attr.Default == nil {
		t.Errorf("scope should have a Default")
	}
	// The framework's RequiresReplace() returns a planmodifier whose
	// Description is "If the value of this attribute changes, Terraform
	// will destroy and recreate the resource." — match on "destroy and
	// recreate" rather than "replace" because the canonical phrasing
	// doesn't use the word "replace" itself.
	foundRequiresReplace := false
	for _, pm := range attr.PlanModifiers {
		desc := strings.ToLower(pm.Description(context.Background()))
		if strings.Contains(desc, "destroy and recreate") || strings.Contains(desc, "replace") {
			foundRequiresReplace = true
			break
		}
	}
	if !foundRequiresReplace {
		t.Errorf("scope should carry RequiresReplace() — backend cannot mutate scope on an existing row")
	}
}

// TestSchema_CreatedByTenantIDComputed asserts the new attribute is
// Computed-only (not user-settable).
func TestSchema_CreatedByTenantIDComputed(t *testing.T) {
	r := New().(*sshKeyResource)
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	attr, ok := resp.Schema.Attributes["created_by_tenant_id"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("created_by_tenant_id missing or wrong type: %T", resp.Schema.Attributes["created_by_tenant_id"])
	}
	if attr.Required || attr.Optional {
		t.Errorf("created_by_tenant_id must be Computed-only, got required=%v optional=%v", attr.Required, attr.Optional)
	}
	if !attr.Computed {
		t.Errorf("created_by_tenant_id must be Computed=true")
	}
	if attr.Sensitive {
		t.Errorf("created_by_tenant_id should NOT be Sensitive (it's just a tenant UUID)")
	}
}
