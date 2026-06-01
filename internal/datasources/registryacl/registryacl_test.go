package registryacl

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries/reg-1/acls", Status: http.StatusOK, Body: []map[string]any{
			{"id": "acl-1", "registry_id": "reg-1", "user_id": "u-1", "username": "ci",
				"repo_pattern": "ci/*", "actions": []string{"pull"}, "created_at": "2026-05-25T10:00:00Z",
				"updated_at": "2026-05-25T10:05:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListRegistryACLs(context.Background(), "reg-1")
	if err != nil {
		t.Fatalf("ListRegistryACLs: %v", err)
	}
	if len(list) != 1 || list[0].RepoPattern != "ci/*" {
		t.Errorf("unexpected: %+v", list)
	}
}
