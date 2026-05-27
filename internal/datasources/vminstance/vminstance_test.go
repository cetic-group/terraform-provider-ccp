package vminstance

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func fixture(id, name, region string) map[string]any {
	return map[string]any{
		"id":                id,
		"name":              name,
		"region":            region,
		"plan":              "small",
		"cores":             2,
		"memory_mb":         4096,
		"disk_gb":           40,
		"template":          "ccp-debian-13",
		"status":            "running",
		"vnet_id":           "vnet-1",
		"has_root_password": false,
		"tags":              []string{"prod"},
		"created_at":        "2026-05-25T10:00:00Z",
		"updated_at":        "2026-05-25T10:05:00Z",
	}
}

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vm-instances/vm-1", Status: http.StatusOK, Body: fixture("vm-1", "app", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetVMInstance(context.Background(), "vm-1")
	if err != nil {
		t.Fatalf("GetVMInstance: %v", err)
	}
	if got.Cores != 2 || got.MemoryMB != 4096 {
		t.Errorf("unexpected sizing: %+v", got)
	}
}

func TestLookupByNameRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vm-instances", Status: http.StatusOK, Body: []map[string]any{
			fixture("vm-a", "app", "RNN"),
			fixture("vm-b", "db", "RNN"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListVMInstances(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListVMInstances: %v", err)
	}
	var found *client.VMInstance
	for i := range list {
		if list[i].Name == "db" {
			found = &list[i]
			break
		}
	}
	if found == nil || found.ID != "vm-b" {
		t.Errorf("expected vm-b, got %+v", found)
	}
}
