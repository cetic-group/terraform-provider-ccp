package objectbucket

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/b-1", Status: http.StatusOK, Body: map[string]any{
			"id": "b-1", "name": "data", "region": "RNN", "size_bytes": 1024, "status": "active",
			"is_public": false, "tags": []string{}, "created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetObjectBucket(context.Background(), "b-1")
	if err != nil {
		t.Fatalf("GetObjectBucket: %v", err)
	}
	if got.SizeBytes != 1024 {
		t.Errorf("expected size 1024, got %d", got.SizeBytes)
	}
}
