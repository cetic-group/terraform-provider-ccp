package loadbalancer

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func fixture(id, name, region string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"region":     region,
		"plan":       "lb-small",
		"vnet_id":    "vnet-1",
		"status":     "active",
		"tags":       []string{},
		"listeners":  []map[string]any{},
		"created_at": "2026-05-25T10:00:00Z",
		"updated_at": "2026-05-25T10:05:00Z",
	}
}

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/load-balancers/lb-1", Status: http.StatusOK, Body: fixture("lb-1", "main", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetLoadBalancer(context.Background(), "lb-1")
	if err != nil {
		t.Fatalf("GetLoadBalancer: %v", err)
	}
	if got.VnetID != "vnet-1" {
		t.Errorf("unexpected vnet_id: %q", got.VnetID)
	}
}

func TestLookupByNameRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/load-balancers", Status: http.StatusOK, Body: []map[string]any{
			fixture("lb-a", "main", "RNN"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListLoadBalancers(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListLoadBalancers: %v", err)
	}
	if len(list) != 1 || list[0].Name != "main" {
		t.Errorf("expected main, got %+v", list)
	}
}
