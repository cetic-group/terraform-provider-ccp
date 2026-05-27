package vmsnapshot

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByName(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vms/vm-1/snapshots", Status: http.StatusOK, Body: []map[string]any{
			{"id": "s-1", "vm_instance_id": "vm-1", "name": "before-upgrade", "status": "available", "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListVmSnapshots(context.Background(), "vm-1")
	if err != nil {
		t.Fatalf("ListVmSnapshots: %v", err)
	}
	if len(list) != 1 || list[0].Name != "before-upgrade" {
		t.Errorf("unexpected: %+v", list)
	}
}
