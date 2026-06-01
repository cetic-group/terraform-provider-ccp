// Tests for ccp_secret — covers the rotate vs PATCH split in Update(),
// `data` preservation across Read() (no implicit reveal-endpoint call),
// 404 idempotence on Delete, and applySecretToModel mapping.
package secret

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// baseFixture returns the JSON shape the API uses for /v1/secrets — note
// the absence of `data`: the API never returns plaintext outside the
// dedicated reveal endpoint.
func baseFixture(id string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        "db-creds",
		"description": "PostgreSQL admin",
		"version":     1,
		"tags":        []string{"env:prod"},
		"created_at":  "2026-05-13T10:00:00Z",
		"updated_at":  "2026-05-13T10:00:00Z",
	}
}

// ─── 1. Create — POST body carries data + payload, response stores metadata ─

func TestCreateSecret_SendsDataAndMetadata(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/secrets", Status: http.StatusCreated, Body: baseFixture("sec-1")},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	desc := "PostgreSQL admin"
	got, err := c.CreateSecret(context.Background(), client.SecretCreatePayload{
		Name:        "db-creds",
		Description: &desc,
		Data:        map[string]string{"password": "s3cr3t", "username": "postgres"},
		Tags:        []string{"env:prod"},
	})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if got.ID != "sec-1" {
		t.Errorf("ID mismatch: got %q", got.ID)
	}
	if got.Version != 1 {
		t.Errorf("Version mismatch: got %d", got.Version)
	}

	// Verify the POST body actually carried `data` + `tags`.
	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	data, ok := sent["data"].(map[string]any)
	if !ok || data["password"] != "s3cr3t" {
		t.Errorf("POST body missing data.password: %v", sent["data"])
	}
	tags, ok := sent["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "env:prod" {
		t.Errorf("POST body missing tags[0]=env:prod: %v", sent["tags"])
	}
}

// ─── 2. Read — GET response never carries `data` ───────────────────────────

func TestReadSecret_NoPlaintextInResponse(t *testing.T) {
	body := baseFixture("sec-1")
	// Explicit absence — the production API NEVER serializes `data` here.

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/secrets/sec-1", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetSecret(context.Background(), "sec-1")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	raw, _ := json.Marshal(got)
	if bytesContains(raw, `"data"`) {
		t.Errorf("Read shape unexpectedly contains data field: %s", string(raw))
	}
}

// ─── 3. Update — PATCH metadata only (no rotate when data unchanged) ───────

func TestUpdateSecret_PatchOnlyMetadata(t *testing.T) {
	updated := baseFixture("sec-1")
	updated["description"] = "PostgreSQL admin v2"
	updated["tags"] = []string{"env:prod", "team:data"}
	updated["updated_at"] = "2026-05-13T11:00:00Z"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/secrets/sec-1", Status: http.StatusOK, Body: updated},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	newDesc := "PostgreSQL admin v2"
	newTags := []string{"env:prod", "team:data"}
	got, err := c.UpdateSecret(context.Background(), "sec-1", client.SecretUpdatePayload{
		Description: &newDesc,
		Tags:        &newTags,
	})
	if err != nil {
		t.Fatalf("UpdateSecret: %v", err)
	}
	if got.Description == nil || *got.Description != "PostgreSQL admin v2" {
		t.Errorf("Description not propagated: %v", got.Description)
	}

	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, hasData := sent["data"]; hasData {
		t.Errorf("PATCH body unexpectedly included `data`: %v", sent)
	}
	tags, ok := sent["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("PATCH body missing tags list: %v", sent["tags"])
	}
}

// ─── 4. Rotate — POST to /rotate bumps version ─────────────────────────────

func TestRotateSecret_BumpsVersion(t *testing.T) {
	rotated := baseFixture("sec-1")
	rotated["version"] = 2
	rotated["last_rotated_at"] = "2026-05-13T11:00:00Z"
	rotated["updated_at"] = "2026-05-13T11:00:00Z"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/secrets/sec-1/rotate", Status: http.StatusOK, Body: rotated},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.RotateSecret(context.Background(), "sec-1", client.SecretRotatePayload{
		Data: map[string]string{"password": "new-s3cr3t"},
	})
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("Version not bumped: got %d, want 2", got.Version)
	}
	if got.LastRotatedAt == nil {
		t.Errorf("LastRotatedAt should be set after rotation")
	}
}

// ─── 5. Delete — 404 is idempotent ─────────────────────────────────────────

func TestDeleteSecret_404IsIdempotent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/secrets/sec-1", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	err := c.DeleteSecret(context.Background(), "sec-1")
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

// ─── 6. GetSecretByName — client-side filter on /v1/secrets ────────────────

func TestGetSecretByName_FiltersClientSide(t *testing.T) {
	list := []map[string]any{
		{
			"id":         "sec-a",
			"name":       "other",
			"version":    1,
			"tags":       []string{},
			"created_at": "2026-05-13T10:00:00Z",
			"updated_at": "2026-05-13T10:00:00Z",
		},
		baseFixture("sec-1"),
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/secrets", Status: http.StatusOK, Body: list},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetSecretByName(context.Background(), "db-creds")
	if err != nil {
		t.Fatalf("GetSecretByName: %v", err)
	}
	if got.ID != "sec-1" {
		t.Errorf("wrong secret picked: got %q", got.ID)
	}
}

func TestGetSecretByName_NotFound(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/secrets", Status: http.StatusOK, Body: []map[string]any{}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	_, err := c.GetSecretByName(context.Background(), "missing")
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

// ─── 7. applySecretToModel — mapping API → state, preserves Data ───────────

func TestApplySecretToModel_PreservesData(t *testing.T) {
	desc := "test"
	lastRot := "2026-05-13T11:00:00Z"
	api := &client.Secret{
		ID:            "sec-1",
		Name:          "db-creds",
		Description:   &desc,
		Version:       3,
		Tags:          []string{"env:prod"},
		LastRotatedAt: &lastRot,
		CreatedAt:     "2026-05-13T10:00:00Z",
		UpdatedAt:     "2026-05-13T11:00:00Z",
	}
	preExistingData := types.MapValueMust(types.StringType, map[string]attr.Value{
		"password": types.StringValue("user-supplied"),
	})
	m := &secretResourceModel{
		Data: preExistingData,
	}
	if diags := applySecretToModel(context.Background(), m, api); diags.HasError() {
		t.Fatalf("applySecretToModel diagnostics: %v", diags)
	}
	// applySecretToModel must NOT touch Data — callers handle that explicitly.
	if !m.Data.Equal(preExistingData) {
		t.Errorf("applySecretToModel clobbered Data: got %v", m.Data)
	}
	if m.ID.ValueString() != "sec-1" {
		t.Errorf("ID mismatch: got %q", m.ID.ValueString())
	}
	if m.Version.ValueInt64() != 3 {
		t.Errorf("Version mismatch: got %d", m.Version.ValueInt64())
	}
	if len(m.Tags.Elements()) != 1 {
		t.Errorf("Tags should have one entry, got %v", m.Tags)
	}
}

func TestApplySecretToModel_NullDescription(t *testing.T) {
	api := &client.Secret{
		ID:        "sec-1",
		Name:      "db-creds",
		Version:   1,
		Tags:      []string{},
		CreatedAt: "2026-05-13T10:00:00Z",
		UpdatedAt: "2026-05-13T10:00:00Z",
	}
	m := &secretResourceModel{}
	if diags := applySecretToModel(context.Background(), m, api); diags.HasError() {
		t.Fatalf("applySecretToModel diagnostics: %v", diags)
	}
	if !m.Description.IsNull() {
		t.Errorf("Description should be Null when API returns no description, got %v", m.Description)
	}
}

// ─── 8. Schema enforcement — name regex ────────────────────────────────────

func TestNameRegex(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"db-creds", true},
		{"a", true},
		{"a-b-c-1-2-3", true},
		// Path-based (Vault KV style) — segments joined by `/`.
		{"prod/db/credentials", true},
		{"team-a/api-tokens/github", true},
		{"a/b", true},
		// Invalid path forms.
		{"/leading-slash", false},
		{"trailing-slash/", false},
		{"double//slash", false},
		{"empty/seg/", false},
		{"DB-creds", false},   // uppercase
		{"-db", false},        // leading dash
		{"1-db", false},       // leading digit
		{"db_creds", false},   // underscore
		{"db.creds", false},   // dot
		{"prod/DB/x", false},  // uppercase segment
		{"prod/1db/x", false}, // leading-digit segment
		{"", false},           // empty
		{"a-very-long-name-that-goes-on-and-on-and-on-past-the-63-char-limit", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nameRegex.MatchString(tc.name)
			if got != tc.valid {
				t.Errorf("nameRegex(%q) = %v, want %v", tc.name, got, tc.valid)
			}
		})
	}
}

func bytesContains(haystack []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
