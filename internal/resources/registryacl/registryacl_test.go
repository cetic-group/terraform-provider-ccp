// Tests for ccp_registry_acl — CRUD with a focus on actions validation
// and PATCH idempotence.
package registryacl

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func aclFixture(id, repo string, actions []string) map[string]any {
	return map[string]any{
		"id":           id,
		"registry_id":  "reg-1",
		"user_id":      "u-1",
		"username":     "ci-pull",
		"repo_pattern": repo,
		"actions":      actions,
		"created_at":   "2026-05-09T10:00:00Z",
		"updated_at":   "2026-05-09T10:00:00Z",
	}
}

func TestCreate_ACL(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/registries/reg-1/acls", Status: http.StatusCreated, Body: aclFixture("a-1", "myapp/*", []string{"pull"})},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.CreateRegistryACL(context.Background(), "reg-1", client.RegistryACLCreateRequest{
		UserID:      "u-1",
		RepoPattern: "myapp/*",
		Actions:     []string{"pull"},
	})
	if err != nil {
		t.Fatalf("CreateRegistryACL: %v", err)
	}
	if got.RepoPattern != "myapp/*" {
		t.Errorf("repo_pattern mismatch")
	}
}

func TestUpdate_ACLPatchIdempotent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/registries/reg-1/acls/a-1", Status: http.StatusOK, Body: aclFixture("a-1", "myapp/*", []string{"pull", "push"})},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.UpdateRegistryACL(context.Background(), "reg-1", "a-1", client.RegistryACLUpdateRequest{
		Actions: []string{"pull", "push"},
	})
	if err != nil {
		t.Fatalf("UpdateRegistryACL: %v", err)
	}
	// Verify body forwarded
	calls := srv.Calls()
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if a, _ := sent["actions"].([]any); len(a) != 2 {
		t.Errorf("expected 2 actions in PATCH body, got %v", sent["actions"])
	}
	if got.RepoPattern != "myapp/*" {
		t.Errorf("repo_pattern lost: %v", got.RepoPattern)
	}
}

func TestList_FindsACL(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries/reg-1/acls", Status: http.StatusOK, Body: []map[string]any{
			aclFixture("a-1", "myapp/*", []string{"pull"}),
			aclFixture("a-2", "*", []string{"pull", "push"}),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	acls, err := c.ListRegistryACLs(context.Background(), "reg-1")
	if err != nil {
		t.Fatalf("ListRegistryACLs: %v", err)
	}
	if len(acls) != 2 {
		t.Fatalf("expected 2 ACLs, got %d", len(acls))
	}
}

func TestDelete_404IsSilent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/registries/reg-1/acls/a-1", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	err := c.DeleteRegistryACL(context.Background(), "reg-1", "a-1")
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestValidator_Actions(t *testing.T) {
	v := setvalidator.ValueStringsAre(stringvalidator.OneOf("pull", "push", "*"))
	for _, tc := range []struct {
		actions []attrLike
		valid   bool
	}{
		{[]attrLike{{val: "pull"}}, true},
		{[]attrLike{{val: "push"}, {val: "*"}}, true},
		{[]attrLike{{val: "delete"}}, false},
	} {
		set, _ := types.SetValueFrom(context.Background(), types.StringType, valuesOf(tc.actions))
		req := validator.SetRequest{ConfigValue: set}
		resp := &validator.SetResponse{}
		v.ValidateSet(context.Background(), req, resp)
		if got := !resp.Diagnostics.HasError(); got != tc.valid {
			t.Errorf("actions=%v valid=%v, got %v: %v", tc.actions, tc.valid, got, resp.Diagnostics)
		}
	}
}

func TestValidator_RepoPattern(t *testing.T) {
	re := repoPatternRE()
	for _, tc := range []struct {
		val   string
		valid bool
	}{
		{"myapp/*", true},
		{"*", true},
		{"library/nginx", true},
		{"My/App", false},  // uppercase
		{"app name", false}, // space
	} {
		got := re.MatchString(tc.val)
		if got != tc.valid {
			t.Errorf("repo=%q valid=%v, got %v", tc.val, tc.valid, got)
		}
	}
}

// helpers ------------------------------------------------------------------

type attrLike struct{ val string }

func valuesOf(list []attrLike) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.val
	}
	return out
}
