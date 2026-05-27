package vnetfirewallrule

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vnets/v-1/firewall/rules", Status: http.StatusOK, Body: []map[string]any{
			{"id": "r-1", "vnet_id": "v-1", "position": 10, "direction": "in", "action": "accept",
				"enabled": true, "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetVnetFirewallRule(context.Background(), "v-1", "r-1")
	if err != nil {
		t.Fatalf("GetVnetFirewallRule: %v", err)
	}
	if got.Action != "accept" {
		t.Errorf("unexpected: %+v", got)
	}
}
