package publicip

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/public-ips", Status: http.StatusOK, Body: []map[string]any{
			{"id": "pip-1", "pool_id": "pool-1", "region": "RNN", "ip_address": "203.0.113.10", "status": "attached", "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetPublicIP(context.Background(), "pip-1")
	if err != nil {
		t.Fatalf("GetPublicIP: %v", err)
	}
	if got.IPAddress != "203.0.113.10" {
		t.Errorf("expected 203.0.113.10, got %q", got.IPAddress)
	}
}
