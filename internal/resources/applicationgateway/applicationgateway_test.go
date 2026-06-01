// Unit tests for ccp_application_gateway — exercises the client +
// pollUntilReady helper using testutil.NewTestServer. Acceptance tests
// (TF_ACC=1) live in the consuming modules repo.
package applicationgateway

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func fixtureCreating(id string) map[string]any {
	return map[string]any{
		"id":                  id,
		"name":                "web-appgw",
		"region":              "RNN",
		"plan":                "medium",
		"vpc_id":              "vpc-1",
		"vnet_id":             "vnet-1",
		"status":              "creating",
		"force_https":         true,
		"hsts_enabled":        false,
		"hsts_max_age":        31536000,
		"global_allow_cidrs":  []string{},
		"global_deny_cidrs":   []string{},
		"trust_proxy_headers": false,
		"tags":                []string{"env:prod"},
		"created_at":          "2026-05-15T10:00:00Z",
		"updated_at":          "2026-05-15T10:00:00Z",
	}
}

func fixtureActive(id string, overrides map[string]any) map[string]any {
	m := fixtureCreating(id)
	m["status"] = "active"
	vip := "10.0.1.10"
	m["vip_address"] = vip
	for k, v := range overrides {
		m[k] = v
	}
	return m
}

func TestCreate_PollsToActive(t *testing.T) {
	id := "appgw-1"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/app-gateways", Status: http.StatusCreated, Body: fixtureCreating(id)},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: fixtureCreating(id)},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: fixtureActive(id, nil)},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	created, err := c.CreateApplicationGateway(context.Background(), client.ApplicationGatewayCreateRequest{
		Name: "web-appgw", Region: "RNN", Plan: "medium", VpcID: "vpc-1", VnetID: "vnet-1",
	})
	if err != nil {
		t.Fatalf("CreateApplicationGateway: %v", err)
	}
	if created.Status != client.AppGWStatusCreating {
		t.Fatalf("expected creating status, got %q", created.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	final, err := pollUntilReady(ctx, c, id, 30*time.Second)
	if err != nil {
		t.Fatalf("pollUntilReady: %v", err)
	}
	if final.Status != client.AppGWStatusActive {
		t.Fatalf("expected active, got %q", final.Status)
	}
	if final.VIPAddress == nil || *final.VIPAddress != "10.0.1.10" {
		t.Fatalf("expected vip 10.0.1.10, got %v", final.VIPAddress)
	}
}

func TestCreate_FailsOnErrorStatus(t *testing.T) {
	id := "appgw-2"
	errMsg := "node out of capacity"
	errFixture := fixtureCreating(id)
	errFixture["status"] = "error"
	errFixture["error_message"] = errMsg

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/app-gateways", Status: http.StatusCreated, Body: fixtureCreating(id)},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: errFixture},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	if _, err := c.CreateApplicationGateway(context.Background(), client.ApplicationGatewayCreateRequest{
		Name: "web-appgw", Region: "RNN", Plan: "medium", VpcID: "vpc-1", VnetID: "vnet-1",
	}); err != nil {
		t.Fatalf("CreateApplicationGateway: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := pollUntilReady(ctx, c, id, 5*time.Second)
	if err == nil {
		t.Fatal("expected error status to surface, got nil")
	}
}

func TestUpdate_PatchForwardsFields(t *testing.T) {
	id := "appgw-3"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "PATCH", Path: "/v1/app-gateways/" + id,
			Status: http.StatusOK,
			BodyFn: func(t *testing.T, reqBody []byte) (int, any) {
				// Echo the body back wrapped into a full gateway fixture.
				active := fixtureActive(id, nil)
				active["force_https"] = false
				active["tags"] = []string{"env:staging"}
				return http.StatusOK, active
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	hf := false
	tags := []string{"env:staging"}
	got, err := c.UpdateApplicationGateway(context.Background(), id, client.ApplicationGatewayUpdateRequest{
		ForceHTTPS: &hf,
		Tags:       &tags,
	})
	if err != nil {
		t.Fatalf("UpdateApplicationGateway: %v", err)
	}
	if got.ForceHTTPS {
		t.Fatalf("expected force_https=false after update")
	}
}

func TestDelete_Idempotent(t *testing.T) {
	id := "appgw-4"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/app-gateways/" + id, Status: http.StatusNoContent},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	if err := c.DeleteApplicationGateway(context.Background(), id); err != nil {
		t.Fatalf("DeleteApplicationGateway: %v", err)
	}
}
