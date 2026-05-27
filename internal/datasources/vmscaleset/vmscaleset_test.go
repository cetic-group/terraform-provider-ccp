package vmscaleset

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vm-scale-sets/ss-1", Status: http.StatusOK, Body: map[string]any{
			"id": "ss-1", "name": "workers", "region": "RNN", "plan": "small", "template": "ccp-debian-13",
			"min_instances": 1, "max_instances": 5, "desired_instances": 3, "auto_repair": true,
			"status": "active", "tags": []string{},
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetVMScaleSet(context.Background(), "ss-1")
	if err != nil {
		t.Fatalf("GetVMScaleSet: %v", err)
	}
	if got.DesiredInstances != 3 {
		t.Errorf("expected 3 desired, got %d", got.DesiredInstances)
	}
}
