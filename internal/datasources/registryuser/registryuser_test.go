package registryuser

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByUsername(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries/reg-1/users", Status: http.StatusOK, Body: []map[string]any{
			{"id": "u-1", "registry_id": "reg-1", "username": "ci", "is_admin": false, "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListRegistryUsers(context.Background(), "reg-1")
	if err != nil {
		t.Fatalf("ListRegistryUsers: %v", err)
	}
	if len(list) != 1 || list[0].Username != "ci" {
		t.Errorf("unexpected: %+v", list)
	}
}
