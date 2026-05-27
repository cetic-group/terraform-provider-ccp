package vnetipresv

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByName(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vnets/v-1/ip-reservations", Status: http.StatusOK, Body: []map[string]any{
			{"id": "r-1", "vnet_id": "v-1", "name": "gateway", "ip": "10.0.0.1", "count": 1, "kind": "single", "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListVnetIpReservations(context.Background(), "v-1")
	if err != nil {
		t.Fatalf("ListVnetIpReservations: %v", err)
	}
	if len(list) != 1 || list[0].Name != "gateway" {
		t.Errorf("unexpected: %+v", list)
	}
}
