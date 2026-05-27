package dbpginstance

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: map[string]any{
			"id": "db-1", "name": "app", "region": "RNN", "engine": "postgresql", "tier": "ha",
			"plan": "dev", "vpc_id": "vpc-1", "vnet_id": "vnet-1", "status": "active",
			"replicas": 2, "storage_gb": 20, "cpu_millicores": 500, "memory_mb": 2048, "tags": []string{},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetDbPg(context.Background(), "db-1")
	if err != nil {
		t.Fatalf("GetDbPg: %v", err)
	}
	if got.Engine != "postgresql" || got.Replicas != 2 {
		t.Errorf("unexpected: %+v", got)
	}
}
