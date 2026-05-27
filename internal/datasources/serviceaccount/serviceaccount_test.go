package serviceaccount

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/service-accounts/sa-1", Status: http.StatusOK, Body: map[string]any{
			"id": "sa-1", "tenant_id": "tn-1", "org_id": "or-1", "name": "ci",
			"token_prefix": "ccp_sa_abcd", "created_at": "2026-05-25T10:00:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetServiceAccount(context.Background(), "sa-1")
	if err != nil {
		t.Fatalf("GetServiceAccount: %v", err)
	}
	if got.TokenPrefix != "ccp_sa_abcd" {
		t.Errorf("unexpected: %+v", got)
	}
}
