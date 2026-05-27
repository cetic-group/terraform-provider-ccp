package objectstoragekey

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/object-storage/keys/k-1", Status: http.StatusOK, Body: map[string]any{
			"id": "k-1", "region": "RNN", "label": "ci", "access_level": "readwrite",
			"access_key_prefix": "ABCDEF12", "created_at": "2026-05-25T10:00:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetObjectStorageKey(context.Background(), "k-1")
	if err != nil {
		t.Fatalf("GetObjectStorageKey: %v", err)
	}
	if got.AccessKeyPrefix != "ABCDEF12" {
		t.Errorf("unexpected: %+v", got)
	}
}
