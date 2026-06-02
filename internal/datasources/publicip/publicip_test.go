package publicip

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/public-ips", Status: http.StatusOK, Body: []map[string]any{
			{"id": "pip-1", "pool_id": "pool-1", "region": "RNN", "ip_address": "203.0.113.10", "status": "attached", "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetPublicIP(context.Background(), "pip-1")
	if err != nil {
		t.Fatalf("GetPublicIP: %v", err)
	}
	if got.IPAddress != "203.0.113.10" {
		t.Errorf("expected 203.0.113.10, got %q", got.IPAddress)
	}
}

func strptr(s string) *string { return &s }

func TestLookupByLabelFound(t *testing.T) {
	list := []client.PublicIP{
		{ID: "pip-1", IPAddress: "203.0.113.10", Label: strptr("passerelle-prod")},
		{ID: "pip-2", IPAddress: "203.0.113.11", Label: strptr("autre")},
		{ID: "pip-3", IPAddress: "203.0.113.12"}, // nil label
	}
	got, summary, detail := selectPublicIP(list, "", "", "passerelle-prod", false, false, true)
	if got == nil {
		t.Fatalf("expected a match, got error %q / %q", summary, detail)
	}
	if got.ID != "pip-1" {
		t.Errorf("expected pip-1, got %q", got.ID)
	}
}

func TestLookupByLabelNotFound(t *testing.T) {
	list := []client.PublicIP{
		{ID: "pip-1", IPAddress: "203.0.113.10", Label: strptr("passerelle-prod")},
		{ID: "pip-3", IPAddress: "203.0.113.12"}, // nil label
	}
	got, summary, _ := selectPublicIP(list, "", "", "nope", false, false, true)
	if got != nil {
		t.Fatalf("expected no match, got %+v", got)
	}
	if summary != "Public IP not found" {
		t.Errorf("expected not-found summary, got %q", summary)
	}
}

func TestLookupByLabelAmbiguous(t *testing.T) {
	list := []client.PublicIP{
		{ID: "pip-1", IPAddress: "203.0.113.10", Label: strptr("dup")},
		{ID: "pip-2", IPAddress: "203.0.113.11", Label: strptr("dup")},
	}
	got, summary, detail := selectPublicIP(list, "", "", "dup", false, false, true)
	if got != nil {
		t.Fatalf("expected ambiguity error, got match %+v", got)
	}
	if summary != "Ambiguous lookup" {
		t.Errorf("expected ambiguous summary, got %q", summary)
	}
	if !strings.Contains(detail, "labels are not unique") {
		t.Errorf("unexpected detail: %q", detail)
	}
}
