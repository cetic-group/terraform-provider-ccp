package vnetpeering

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vnet-peerings/p-1", Status: http.StatusOK, Body: map[string]any{
			"id":         "p-1",
			"name":       "frontend-backend",
			"vnet_a_id":  "vnet-a",
			"vnet_b_id":  "vnet-b",
			"status":     "active",
			"tags":       []string{"core"},
			"created_at": "2026-05-25T10:00:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetVnetPeering(context.Background(), "p-1")
	if err != nil {
		t.Fatalf("GetVnetPeering: %v", err)
	}
	if got.VnetAID != "vnet-a" || got.VnetBID != "vnet-b" {
		t.Errorf("unexpected vnet ids: %+v", got)
	}
}
