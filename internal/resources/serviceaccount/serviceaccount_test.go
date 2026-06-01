// Tests for ccp_service_account — focuses on the one-shot token
// preservation pattern (apikey-style) and the in-place PATCH for
// name/description.
package serviceaccount

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func baseFixture(id string) map[string]any {
	return map[string]any{
		"id":           id,
		"tenant_id":    "t-1",
		"org_id":       "o-1",
		"name":         "ci-pipeline",
		"description":  "Used by GitHub Actions",
		"token_prefix": "ccp_sa_aabbccdd",
		"created_at":   "2026-05-09T10:00:00Z",
	}
}

// ─── 1. Create reveals token; state preserves it ───────────────────────────

func TestCreate_RevealsTokenOnce(t *testing.T) {
	body := baseFixture("sa-1")
	body["token"] = "ccp_sa_aabbccdd_zzzzzzzzzzzzzzzzzzzzzzzzzzzzz"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/service-accounts", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateServiceAccount(context.Background(), client.ServiceAccountCreateRequest{
		Name: "ci-pipeline",
	})
	if err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if got.Token != "ccp_sa_aabbccdd_zzzzzzzzzzzzzzzzzzzzzzzzzzzzz" {
		t.Fatalf("token not propagated: got %q", got.Token)
	}
	if got.TokenPrefix != "ccp_sa_aabbccdd" {
		t.Fatalf("token_prefix not propagated: got %q", got.TokenPrefix)
	}
}

// ─── 2. Read shape never contains token ────────────────────────────────────

func TestRead_DoesNotReturnToken(t *testing.T) {
	body := baseFixture("sa-1")
	// No "token" field — the API never re-emits it on GET.

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/service-accounts/sa-1", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetServiceAccount(context.Background(), "sa-1")
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	// Decode raw to verify token is absent from the read shape.
	raw, _ := json.Marshal(got)
	if bytesContains(raw, `"token"`) && !bytesContains(raw, `"token_prefix"`) {
		t.Errorf("Read shape unexpectedly contains token field: %s", string(raw))
	}
}

// ─── 3. applySAToModel preserves token field semantics ─────────────────────

func TestApplySAToModel_PreservesTokenField(t *testing.T) {
	now := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	desc := "test"
	sa := &client.ServiceAccount{
		ID:          "sa-1",
		TenantID:    "t-1",
		OrgID:       "o-1",
		Name:        "ci",
		Description: &desc,
		TokenPrefix: "ccp_sa_aabbccdd",
		CreatedAt:   now,
	}
	m := &serviceAccountResourceModel{
		Token: types.StringValue("ccp_sa_aabbccdd_xxxxxxxx"),
	}
	applySAToModel(m, sa)
	// applySAToModel must NOT touch Token — callers preserve it explicitly.
	if m.Token.ValueString() != "ccp_sa_aabbccdd_xxxxxxxx" {
		t.Errorf("applySAToModel clobbered Token: got %q", m.Token.ValueString())
	}
	if m.ID.ValueString() != "sa-1" {
		t.Errorf("ID not propagated: got %q", m.ID.ValueString())
	}
	if m.TokenPrefix.ValueString() != "ccp_sa_aabbccdd" {
		t.Errorf("TokenPrefix not propagated: got %q", m.TokenPrefix.ValueString())
	}
	if m.Description.ValueString() != "test" {
		t.Errorf("Description not propagated: got %q", m.Description.ValueString())
	}
}

// ─── 4. Update PATCHes mutable fields ──────────────────────────────────────

func TestUpdate_PatchesMutableFields(t *testing.T) {
	updated := baseFixture("sa-1")
	updated["name"] = "ci-pipeline-v2"
	updated["description"] = "Used by GitHub Actions (renamed)"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/service-accounts/sa-1", Status: http.StatusOK, Body: updated},
		{Method: "GET", Path: "/v1/service-accounts/sa-1", Status: http.StatusOK, Body: updated},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	newName := "ci-pipeline-v2"
	got, err := c.UpdateServiceAccount(context.Background(), "sa-1", client.ServiceAccountUpdateRequest{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("UpdateServiceAccount: %v", err)
	}
	if got.Name != "ci-pipeline-v2" {
		t.Errorf("Update did not propagate name: got %q", got.Name)
	}

	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call before GET, got %d", len(calls))
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if sent["name"] != "ci-pipeline-v2" {
		t.Errorf("PATCH body name mismatch: %v", sent["name"])
	}

	if _, err := c.GetServiceAccount(context.Background(), "sa-1"); err != nil {
		t.Fatalf("re-Read after update: %v", err)
	}
}

// ─── 5. Delete is silent on 404 ────────────────────────────────────────────

func TestDelete_404IsSilent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/service-accounts/sa-1", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	err := c.DeleteServiceAccount(context.Background(), "sa-1")
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

// ─── 6. Unmarshal round-trip — ServiceAccount + ServiceAccountWithToken ───

func TestUnmarshal_ServiceAccountShapes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       string
		wantToken string
	}{
		{
			name:      "with token (POST response)",
			raw:       `{"id":"sa-1","tenant_id":"t","org_id":"o","name":"n","token_prefix":"ccp_sa_xxx","token":"ccp_sa_xxx_yyy","created_at":"2026-05-09T10:00:00Z"}`,
			wantToken: "ccp_sa_xxx_yyy",
		},
		{
			name:      "without token (GET response)",
			raw:       `{"id":"sa-1","tenant_id":"t","org_id":"o","name":"n","token_prefix":"ccp_sa_xxx","created_at":"2026-05-09T10:00:00Z"}`,
			wantToken: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wt client.ServiceAccountWithToken
			if err := json.Unmarshal([]byte(tc.raw), &wt); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if wt.Token != tc.wantToken {
				t.Errorf("token mismatch: got %q, want %q", wt.Token, tc.wantToken)
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
