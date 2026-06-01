package dbferretdbinstance

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/db/ferretdb/db-1", Status: http.StatusOK, Body: map[string]any{
			"id": "db-1", "name": "app", "region": "RNN", "engine": "ferretdb", "tier": "single",
			"plan": "dev", "vpc_id": "vpc-1", "vnet_id": "vnet-1", "status": "active",
			"replicas": 1, "storage_gb": 10, "cpu_millicores": 250, "memory_mb": 1024, "tags": []string{},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetDbFerretdb(context.Background(), "db-1")
	if err != nil {
		t.Fatalf("GetDbFerretdb: %v", err)
	}
	if got.Engine != "ferretdb" {
		t.Errorf("expected ferretdb, got %q", got.Engine)
	}
}
