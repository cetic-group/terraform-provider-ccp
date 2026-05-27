package apikey

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/api-keys/k-1", Status: http.StatusOK, Body: map[string]any{
			"id": "k-1", "name": "ci", "prefix": "ccp_live_abcd", "scopes": []string{"read"},
			"created_at": "2026-05-25T10:00:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetApiKey(context.Background(), "k-1")
	if err != nil {
		t.Fatalf("GetApiKey: %v", err)
	}
	if got.Prefix != "ccp_live_abcd" {
		t.Errorf("unexpected: %+v", got)
	}
}
