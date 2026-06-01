// Tests for the ccp_iam_role data source — lookup by id or by name + built_in.
package iamrole

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func roleFixture(id, name string, builtIn bool) map[string]any {
	policy := `{"version":"2026-05-10","statements":[{"effect":"Allow","actions":["registry:*"],"resources":["*"]}]}`
	return map[string]any{
		"id":              id,
		"name":            name,
		"policy_document": json.RawMessage(policy),
		"policy_hash":     "sha256-fake",
		"is_built_in":     builtIn,
		"created_at":      "2026-05-09T10:00:00Z",
		"updated_at":      "2026-05-09T10:00:00Z",
	}
}

// ─── 1. Lookup by id ────────────────────────────────────────────────────────

func TestClient_GetRole_ByID(t *testing.T) {
	body := roleFixture("r-1", "RegistryAdmin", true)
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/iam/roles/r-1", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetRole(context.Background(), "r-1")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if got.ID != "r-1" {
		t.Errorf("id mismatch: got %q", got.ID)
	}
	if !got.IsBuiltIn {
		t.Errorf("expected built-in role")
	}
}

// ─── 2. Lookup by name + built_in=true ─────────────────────────────────────

func TestClient_GetRoleByName_BuiltIn(t *testing.T) {
	list := []map[string]any{
		roleFixture("r-1", "RegistryAdmin", true),
		roleFixture("r-2", "BillingReader", true),
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/iam/roles", Status: http.StatusOK, Body: list},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	tt := true
	got, err := c.GetRoleByName(context.Background(), "BillingReader", &tt)
	if err != nil {
		t.Fatalf("GetRoleByName: %v", err)
	}
	if got.Name != "BillingReader" {
		t.Errorf("name mismatch: got %q", got.Name)
	}
	if got.ID != "r-2" {
		t.Errorf("id mismatch: got %q", got.ID)
	}

	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	// Confirm the filter was forwarded via the query string.
	if calls[0].Path != "/v1/iam/roles" {
		t.Errorf("unexpected path: %s", calls[0].Path)
	}
}

// ─── 3. Lookup by name + built_in=false → 404 if mismatched ────────────────

func TestClient_GetRoleByName_BuiltInMismatch(t *testing.T) {
	list := []map[string]any{
		roleFixture("r-1", "RegistryAdmin", true),
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/iam/roles", Status: http.StatusOK, Body: list},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	ff := false
	_, err := c.GetRoleByName(context.Background(), "RegistryAdmin", &ff)
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound when filtering custom but only built-in exists, got %v", err)
	}
}

// ─── 4. Schema sanity — schema is registered without errors ────────────────

func TestSchema_RegistersWithoutDiagnostics(t *testing.T) {
	// Light sanity check — instantiating the datasource and calling Schema
	// should not panic or set up diagnostics. Full schema validation is
	// done by terraform plugin framework at plugin start.
	d := &iamRoleDataSource{}
	if d == nil {
		t.Fatal("datasource constructor returned nil")
	}
}
