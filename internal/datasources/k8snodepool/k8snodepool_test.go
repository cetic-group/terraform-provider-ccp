package k8snodepool

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/c-1/node-pools/np-1", Status: http.StatusOK, Body: map[string]any{
			"id": "np-1", "cluster_id": "c-1", "name": "workers", "plan": "small", "replicas": 3,
			"labels": map[string]string{"role": "worker"}, "taints": []map[string]any{}, "status": "active",
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetK8sNodePool(context.Background(), "c-1", "np-1")
	if err != nil {
		t.Fatalf("GetK8sNodePool: %v", err)
	}
	if got.Replicas != 3 {
		t.Errorf("expected replicas 3, got %d", got.Replicas)
	}
}
