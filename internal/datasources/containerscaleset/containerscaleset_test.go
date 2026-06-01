package containerscaleset

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/container-scale-sets/ss-1", Status: http.StatusOK, Body: map[string]any{
			"id": "ss-1", "name": "edges", "region": "RNN", "plan": "small", "template": "alpine-3.20",
			"min_instances": 1, "max_instances": 5, "desired_instances": 2, "auto_repair": false,
			"status": "active", "tags": []string{},
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetContainerScaleSet(context.Background(), "ss-1")
	if err != nil {
		t.Fatalf("GetContainerScaleSet: %v", err)
	}
	if got.MinInstances != 1 || got.MaxInstances != 5 {
		t.Errorf("unexpected bounds: %+v", got)
	}
}
