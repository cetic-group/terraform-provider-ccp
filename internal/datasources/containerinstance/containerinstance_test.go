package containerinstance

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
		"cores":             1,
		"memory_mb":         1024,
		"disk_gb":           10,
		"template":          "alpine-3.20",
		"status":            "running",
		"has_root_password": false,
		"tags":              []string{},
		"created_at":        "2026-05-25T10:00:00Z",
		"updated_at":        "2026-05-25T10:05:00Z",
	}
}

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/containers/ct-1", Status: http.StatusOK, Body: fixture("ct-1", "edge", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetContainer(context.Background(), "ct-1")
	if err != nil {
		t.Fatalf("GetContainer: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("expected running, got %q", got.Status)
	}
}

func TestLookupByNameRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/containers", Status: http.StatusOK, Body: []map[string]any{
			fixture("ct-a", "edge", "RNN"),
			fixture("ct-b", "edge", "PAR"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListContainers(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	var matches int
	for _, ct := range list {
		if ct.Name == "edge" && ct.Region == "RNN" {
			matches++
		}
	}
	if matches != 1 {
		t.Errorf("expected 1 match, got %d", matches)
	}
}
