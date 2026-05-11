// Tests for ccp_iam_role — focuses on the client method shapes, the
// setStateFromAPI helper (which must not clobber user-written
// PolicyDocumentJSON when the API canonicalises and re-orders keys),
// and the standard CRUD lifecycle through the httptest mock.
package iamrole

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const samplePolicy = `{"version":"2026-05-10","statements":[{"effect":"Allow","actions":["registry:Pull"],"resources":["arn:ccp:registry:rnn:11111111-2222-3333-4444-555555555555:registry/myreg"]}]}`

// Same policy, re-ordered keys (simulates what the API may return after JCS canonicalisation).
const sampleCanonicalPolicy = `{"statements":[{"actions":["registry:Pull"],"effect":"Allow","resources":["arn:ccp:registry:rnn:11111111-2222-3333-4444-555555555555:registry/myreg"]}],"version":"2026-05-10"}`

func baseRoleFixture(id string) map[string]any {
	return map[string]any{
		"id":              id,
		"tenant_id":       "11111111-2222-3333-4444-555555555555",
		"org_id":          "22222222-3333-4444-5555-666666666666",
		"name":            "RegistryPuller",
		"description":     "Custom role: pull-only on myreg",
		"policy_document": json.RawMessage(samplePolicy),
		"policy_hash":     "abcdef1234567890",
		"is_built_in":    false,
		"created_at":      "2026-05-11T10:00:00Z",
		"updated_at":      "2026-05-11T10:00:00Z",
	}
}

// ─── 1. Create round-trip ─────────────────────────────────────────────────

func TestCreate_Role(t *testing.T) {
	body := baseRoleFixture("role-1")

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/iam/roles", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	desc := "Custom role: pull-only on myreg"
	got, err := c.CreateRole(context.Background(), client.RoleCreateRequest{
		Name:           "RegistryPuller",
		Description:    &desc,
		PolicyDocument: json.RawMessage(samplePolicy),
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if got.ID != "role-1" {
		t.Errorf("id mismatch: %q", got.ID)
	}
	if got.Name != "RegistryPuller" {
		t.Errorf("name mismatch: %q", got.Name)
	}
	if got.IsBuiltIn {
		t.Errorf("custom role marked as built-in")
	}
	if got.PolicyHash != "abcdef1234567890" {
		t.Errorf("policy_hash not propagated: %q", got.PolicyHash)
	}
}

// ─── 2. Read shape ────────────────────────────────────────────────────────

func TestRead_Role(t *testing.T) {
	body := baseRoleFixture("role-1")

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/iam/roles/role-1", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetRole(context.Background(), "role-1")
	if err != nil {
		t.Fatalf("GetRole: %v", err)
	}
	if got.Name != "RegistryPuller" {
		t.Errorf("name not propagated: %q", got.Name)
	}
}

// ─── 3. Update PATCH then re-Read ─────────────────────────────────────────

func TestUpdate_Role_PatchesAndRefetches(t *testing.T) {
	updated := baseRoleFixture("role-1")
	updated["description"] = "Renamed description"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/iam/roles/role-1", Status: http.StatusOK, Body: updated},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	newDesc := "Renamed description"
	got, err := c.UpdateRole(context.Background(), "role-1", client.RoleUpdateRequest{
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if got.Description == nil || *got.Description != "Renamed description" {
		t.Errorf("description not propagated: %v", got.Description)
	}

	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 PATCH call, got %d", len(calls))
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode PATCH body: %v", err)
	}
	if sent["description"] != "Renamed description" {
		t.Errorf("PATCH body description mismatch: %v", sent["description"])
	}
	// Name and policy_document must NOT be in the patch (only changed fields).
	if _, ok := sent["name"]; ok {
		t.Errorf("PATCH body unexpectedly carries name field")
	}
	if _, ok := sent["policy_document"]; ok {
		t.Errorf("PATCH body unexpectedly carries policy_document field")
	}
}

// ─── 4. Delete is silent on 404 (idempotent terraform destroy) ────────────

func TestDelete_Role_404IsSilent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/iam/roles/role-1", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	err := c.DeleteRole(context.Background(), "role-1")
	if !client.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

// ─── 5. setStateFromAPI propagates the API's policy_document bytes ──────
//
// Contract: setStateFromAPI writes the API's returned policy_document
// (possibly re-ordered after JCS canonicalisation) into state. The
// JSONNormalizeEqual plan modifier (declared on the schema attribute)
// compares state and plan semantically — when they're JSON-equivalent,
// it suppresses the diff regardless of key order. So setStateFromAPI
// being "lossy" on ordering is fine and intentional.

func TestSetStateFromAPI_PropagatesAPIBody(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	desc := "test"
	tid := "11111111-2222-3333-4444-555555555555"
	oid := "22222222-3333-4444-5555-666666666666"

	role := &client.Role{
		ID:             "role-1",
		TenantID:       &tid,
		OrgID:          &oid,
		Name:           "RegistryPuller",
		Description:    &desc,
		PolicyDocument: json.RawMessage(sampleCanonicalPolicy), // API returns canonical form
		PolicyHash:     "abcdef1234567890",
		IsBuiltIn:      false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m := &iamRoleResourceModel{
		PolicyDocumentJSON: types.StringValue(samplePolicy), // user-written, pre-API
	}
	setStateFromAPI(m, role)

	// Server-assigned fields propagate.
	if m.ID.ValueString() != "role-1" {
		t.Errorf("ID not propagated: %q", m.ID.ValueString())
	}
	if m.PolicyHash.ValueString() != "abcdef1234567890" {
		t.Errorf("PolicyHash not propagated: %q", m.PolicyHash.ValueString())
	}
	if m.Name.ValueString() != "RegistryPuller" {
		t.Errorf("Name not propagated: %q", m.Name.ValueString())
	}
	if m.IsBuiltIn.ValueBool() {
		t.Errorf("IsBuiltIn unexpectedly true")
	}
	// setStateFromAPI propagates the API's canonical bytes into state.
	// The plan modifier compares semantically upstream to suppress diffs.
	if m.PolicyDocumentJSON.ValueString() != sampleCanonicalPolicy {
		t.Errorf("setStateFromAPI did not propagate API policy_document: got %q, want %q",
			m.PolicyDocumentJSON.ValueString(), sampleCanonicalPolicy)
	}
	// Sanity: the API form and the user form are semantically equal
	// (this is what JSONNormalizeEqual asserts at plan-time).
	var apiObj, userObj map[string]any
	if err := json.Unmarshal([]byte(m.PolicyDocumentJSON.ValueString()), &apiObj); err != nil {
		t.Fatalf("API form not valid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(samplePolicy), &userObj); err != nil {
		t.Fatalf("user form not valid JSON: %v", err)
	}
	apiBytes, _ := json.Marshal(apiObj)
	userBytes, _ := json.Marshal(userObj)
	if string(apiBytes) != string(userBytes) {
		t.Errorf("API and user JSON not semantically equal after canonicalisation:\n  api: %s\n  user: %s", apiBytes, userBytes)
	}
}

// ─── 6. Built-in role update is refused by API (409) — client surfaces err ─

func TestUpdate_BuiltInRole_API409(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/iam/roles/built-in-1", Status: http.StatusConflict, Body: map[string]any{"detail": "Cannot update a built-in role"}},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	newName := "x"
	_, err := c.UpdateRole(context.Background(), "built-in-1", client.RoleUpdateRequest{Name: &newName})
	if err == nil {
		t.Fatalf("expected API error on built-in update, got nil")
	}
}
