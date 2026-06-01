package containersnapshot

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByName(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/containers/ct-1/snapshots", Status: http.StatusOK, Body: []map[string]any{
			{"id": "s-1", "container_id": "ct-1", "name": "pre-upgrade", "status": "available", "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListContainerSnapshots(context.Background(), "ct-1")
	if err != nil {
		t.Fatalf("ListContainerSnapshots: %v", err)
	}
	if len(list) != 1 || list[0].Name != "pre-upgrade" {
		t.Errorf("unexpected: %+v", list)
	}
}
