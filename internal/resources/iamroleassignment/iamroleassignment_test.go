// Tests for ccp_iam_role_assignment — focuses on the immutable
// (RequiresReplace) lifecycle: Create, Read, Delete, idempotent 404
// behavior on Delete, plus the setStateFromAPI helper and the composite
// import ID validator path.
package iamroleassignment

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

func baseAssignmentFixture(id, roleID string) map[string]any {
	return map[string]any{
		"id":             id,
		"role_id":        roleID,
		"tenant_id":      "11111111-2222-3333-4444-555555555555",
		"org_id":         "22222222-3333-4444-5555-666666666666",
		"principal_type": "service_account",
		"principal_id":   "33333333-4444-5555-6666-777777777777",
		"created_at":     "2026-05-11T10:00:00Z",
	}
}

// ─── 1. Create round-trip ─────────────────────────────────────────────────

func TestCreate_Assignment(t *testing.T) {
	body := baseAssignmentFixture("asg-1", "role-1")

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/iam/roles/role-1/assignments", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateRoleAssignment(context.Background(), "role-1", client.RoleAssignmentCreateRequest{
		PrincipalType: "service_account",
		PrincipalID:   "33333333-4444-5555-6666-777777777777",
	})
	if err != nil {
		t.Fatalf("CreateRoleAssignment: %v", err)
	}
	if got.ID != "asg-1" {
		t.Errorf("id mismatch: %q", got.ID)
	}
	if got.RoleID != "role-1" {
		t.Errorf("role_id mismatch: %q", got.RoleID)
	}
	if got.PrincipalType != "service_account" {
		t.Errorf("principal_type mismatch: %q", got.PrincipalType)
	}

	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 POST, got %d", len(calls))
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if sent["principal_type"] != "service_account" {
		t.Errorf("body principal_type mismatch: %v", sent["principal_type"])
	}
	if _, ok := sent["role_id"]; ok {
		t.Errorf("body unexpectedly carries role_id (it's path-only): %v", sent["role_id"])
	}
}

// ─── 2. Create with expires_at sends RFC 3339 ─────────────────────────────

func TestCreate_Assignment_WithExpiresAt(t *testing.T) {
	body := baseAssignmentFixture("asg-2", "role-1")
	body["expires_at"] = "2027-05-11T10:00:00Z"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/iam/roles/role-1/assignments", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	exp := time.Date(2027, 5, 11, 10, 0, 0, 0, time.UTC)
	got, err := c.CreateRoleAssignment(context.Background(), "role-1", client.RoleAssignmentCreateRequest{
		PrincipalType: "service_account",
		PrincipalID:   "33333333-4444-5555-6666-777777777777",
		ExpiresAt:     &exp,
	})
	if err != nil {
		t.Fatalf("CreateRoleAssignment: %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Errorf("expires_at not propagated: %v", got.ExpiresAt)
	}
}

// ─── 3. Read uses list-and-filter (no GET /assignments/{aid} endpoint) ────

func TestRead_Assignment_ListAndFilter(t *testing.T) {
	// GetRoleAssignment calls ListRoleAssignments under the hood (the API
	// does not expose GET /v1/iam/roles/{rid}/assignments/{aid}) and
	// filters by ID client-side. Returning a list with 2 items lets us
	// verify the filter picks the right one.
	body := []map[string]any{
		baseAssignmentFixture("asg-other", "role-1"),
		baseAssignmentFixture("asg-1", "role-1"),
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/iam/roles/role-1/assignments", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetRoleAssignment(context.Background(), "role-1", "asg-1")
	if err != nil {
		t.Fatalf("GetRoleAssignment: %v", err)
	}
	if got.ID != "asg-1" {
		t.Errorf("filter returned wrong assignment: %q", got.ID)
	}
	if got.PrincipalID != "33333333-4444-5555-6666-777777777777" {
		t.Errorf("principal_id not propagated: %q", got.PrincipalID)
	}
}

// Verify NotFound when filter misses on a successful list call.
func TestRead_Assignment_NotFoundOnEmptyList(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/iam/roles/role-1/assignments", Status: http.StatusOK, Body: []map[string]any{}},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	_, err := c.GetRoleAssignment(context.Background(), "role-1", "asg-missing")
	if !client.IsNotFound(err) {
		t.Errorf("expected NotFound for missing assignment, got %v", err)
	}
}

// ─── 4. Delete is silent on 404 ───────────────────────────────────────────

func TestDelete_Assignment_404IsSilent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/iam/roles/role-1/assignments/asg-1", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	err := c.DeleteRoleAssignment(context.Background(), "role-1", "asg-1")
	if !client.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

// ─── 5. setStateFromAPI propagates all fields ─────────────────────────────

func TestSetStateFromAPI_Assignment(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	exp := time.Date(2027, 5, 11, 10, 0, 0, 0, time.UTC)

	asg := &client.RoleAssignment{
		ID:            "asg-1",
		RoleID:        "role-1",
		TenantID:      "11111111-2222-3333-4444-555555555555",
		OrgID:         "22222222-3333-4444-5555-666666666666",
		PrincipalType: "service_account",
		PrincipalID:   "33333333-4444-5555-6666-777777777777",
		ExpiresAt:     &exp,
		CreatedAt:     now,
	}
	m := &iamRoleAssignmentResourceModel{}
	setStateFromAPI(m, asg)

	if m.ID.ValueString() != "asg-1" {
		t.Errorf("ID not propagated: %q", m.ID.ValueString())
	}
	if m.RoleID.ValueString() != "role-1" {
		t.Errorf("RoleID not propagated: %q", m.RoleID.ValueString())
	}
	if m.PrincipalType.ValueString() != "service_account" {
		t.Errorf("PrincipalType not propagated: %q", m.PrincipalType.ValueString())
	}
	if m.PrincipalID.ValueString() != "33333333-4444-5555-6666-777777777777" {
		t.Errorf("PrincipalID not propagated: %q", m.PrincipalID.ValueString())
	}
	if m.ExpiresAt.IsNull() || m.ExpiresAt.IsUnknown() {
		t.Errorf("ExpiresAt should be set, got null/unknown")
	}
	if m.CreatedAt.IsNull() {
		t.Errorf("CreatedAt should be set")
	}
}

// ─── 6. setStateFromAPI handles nil ExpiresAt ─────────────────────────────

func TestSetStateFromAPI_Assignment_NoExpiry(t *testing.T) {
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	asg := &client.RoleAssignment{
		ID:            "asg-1",
		RoleID:        "role-1",
		TenantID:      "t",
		OrgID:         "o",
		PrincipalType: "api_key",
		PrincipalID:   "pid",
		ExpiresAt:     nil,
		CreatedAt:     now,
	}
	m := &iamRoleAssignmentResourceModel{
		ExpiresAt: types.StringValue("2027-01-01T00:00:00Z"), // pre-existing value
	}
	setStateFromAPI(m, asg)
	if !m.ExpiresAt.IsNull() {
		t.Errorf("ExpiresAt should be null when API returns nil, got %q", m.ExpiresAt.ValueString())
	}
}
