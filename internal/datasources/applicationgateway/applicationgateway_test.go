// Unit tests for the ccp_application_gateway data source. Exercises both
// lookup modes (by id, by name+region) via testutil.NewTestServer.
package applicationgateway

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func gwFixture(id, name, region string) map[string]any {
	return map[string]any{
		"id":                  id,
		"name":                name,
		"region":              region,
		"plan":                "medium",
		"vpc_id":              "vpc-1",
		"vnet_id":             "vnet-1",
		"status":              "active",
		"force_https":         true,
		"hsts_enabled":        false,
		"hsts_max_age":        31536000,
		"global_allow_cidrs":  []string{},
		"global_deny_cidrs":   []string{},
		"trust_proxy_headers": false,
		"tags":                []string{},
		"created_at":          "2026-05-15T10:00:00Z",
		"updated_at":          "2026-05-15T10:00:00Z",
		"listeners": []map[string]any{
			{"id": "l1", "appgw_id": id, "hostname": "api.example.com", "custom_domain": true, "acme_status": "issued", "created_at": "2026-05-15T10:01:00Z"},
		},
	}
}

func TestLookup_ByID(t *testing.T) {
	id := "appgw-1"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: gwFixture(id, "web", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.GetApplicationGateway(context.Background(), id)
	if err != nil {
		t.Fatalf("GetApplicationGateway: %v", err)
	}
	if got.Name != "web" {
		t.Errorf("expected name=web, got %q", got.Name)
	}
	if len(got.Listeners) != 1 || got.Listeners[0].Hostname != "api.example.com" {
		t.Errorf("expected 1 listener, got %+v", got.Listeners)
	}
}

func TestLookup_ByNameAndRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "GET", Path: "/v1/app-gateways",
			Status: http.StatusOK,
			Body: []map[string]any{
				gwFixture("appgw-a", "alpha", "RNN"),
				gwFixture("appgw-b", "beta", "RNN"),
			},
		},
		{Method: "GET", Path: "/v1/app-gateways/appgw-b", Status: http.StatusOK, Body: gwFixture("appgw-b", "beta", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	list, err := c.ListApplicationGateways(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListApplicationGateways: %v", err)
	}
	var match *client.ApplicationGateway
	for i := range list {
		if list[i].Name == "beta" {
			m := list[i]
			match = &m
			break
		}
	}
	if match == nil {
		t.Fatalf("expected to find 'beta', got %d gateways", len(list))
	}
	full, err := c.GetApplicationGateway(context.Background(), match.ID)
	if err != nil {
		t.Fatalf("GetApplicationGateway: %v", err)
	}
	if full.ID != "appgw-b" {
		t.Errorf("expected appgw-b, got %q", full.ID)
	}
}
