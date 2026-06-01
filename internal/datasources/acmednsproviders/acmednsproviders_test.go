package acmednsproviders

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestReadCatalog(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/load-balancers/acme/dns-providers", Status: http.StatusOK, Body: map[string]any{
			"cloudflare": map[string]any{"label": "Cloudflare", "fields": []string{"api_token"}},
			"route53":    map[string]any{"label": "AWS Route 53", "fields": []string{"access_key_id", "secret_access_key"}},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	catalog, err := c.ListLBAcmeDNSProviders(context.Background())
	if err != nil {
		t.Fatalf("ListLBAcmeDNSProviders: %v", err)
	}
	if len(catalog) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(catalog))
	}
	cf, ok := catalog["cloudflare"]
	if !ok {
		t.Fatalf("missing cloudflare entry")
	}
	if cf.Label != "Cloudflare" {
		t.Errorf("label: %q", cf.Label)
	}
	if len(cf.Fields) != 1 || cf.Fields[0] != "api_token" {
		t.Errorf("fields: %+v", cf.Fields)
	}
	r53 := catalog["route53"]
	if len(r53.Fields) != 2 {
		t.Errorf("route53 fields: %+v", r53.Fields)
	}
}
