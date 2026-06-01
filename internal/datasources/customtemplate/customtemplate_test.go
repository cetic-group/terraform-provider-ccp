package customtemplate

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/custom-templates/ct-1", Status: http.StatusOK, Body: map[string]any{
			"id": "ct-1", "name": "golden", "template_type": "vm", "region": "RNN", "status": "available",
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetCustomTemplate(context.Background(), "ct-1")
	if err != nil {
		t.Fatalf("GetCustomTemplate: %v", err)
	}
	if got.TemplateType != "vm" {
		t.Errorf("expected vm, got %q", got.TemplateType)
	}
}
