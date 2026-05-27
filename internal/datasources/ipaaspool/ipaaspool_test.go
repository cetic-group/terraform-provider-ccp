package ipaaspool

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/admin/ipaas/pools/p-1", Status: http.StatusOK, Body: map[string]any{
			"id": "p-1", "region": "RNN", "cidr": "192.0.2.0/24", "gateway": "192.0.2.1",
			"kind": "public", "is_active": true, "created_at": "2026-05-25T10:00:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetIpaasPool(context.Background(), "p-1")
	if err != nil {
		t.Fatalf("GetIpaasPool: %v", err)
	}
	if got.CIDR != "192.0.2.0/24" {
		t.Errorf("unexpected: %+v", got)
	}
}
