// Unit tests for ccp_appgw_route — covers the create/update/get loop on
// the API + the helpers that translate between Terraform model and API
// payload for nested header_match / basic_auth_user / map fields.
package appgwroute

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestApiHeaderMatches_DefaultsOpToEq(t *testing.T) {
	in := []headerMatchModel{
		{Name: types.StringValue("X-A"), Value: types.StringValue("v1")},                       // no op → defaults to "eq"
		{Name: types.StringValue("X-B"), Value: types.StringValue("v2"), Op: types.StringValue("prefix")},
	}
	out := apiHeaderMatches(in)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if out[0].Op != "eq" {
		t.Errorf("expected default op=eq, got %q", out[0].Op)
	}
	if out[1].Op != "prefix" {
		t.Errorf("expected op=prefix preserved, got %q", out[1].Op)
	}
}

func TestApiBasicAuthUsers_Roundtrip(t *testing.T) {
	in := []basicAuthUserModel{
		{User: types.StringValue("admin"), Password: types.StringValue("hunter2")},
	}
	out := apiBasicAuthUsers(in)
	if len(out) != 1 || out[0].User != "admin" || out[0].Password != "hunter2" {
		t.Fatalf("unexpected roundtrip: %+v", out)
	}
}

func TestHeaderMatchesChanged(t *testing.T) {
	a := []headerMatchModel{{Name: types.StringValue("X"), Value: types.StringValue("v"), Op: types.StringValue("eq")}}
	b := []headerMatchModel{{Name: types.StringValue("X"), Value: types.StringValue("v"), Op: types.StringValue("eq")}}
	if headerMatchesChanged(a, b) {
		t.Fatal("expected no change between equal slices")
	}
	b[0].Value = types.StringValue("w")
	if !headerMatchesChanged(a, b) {
		t.Fatal("expected change when value differs")
	}
	if !headerMatchesChanged(a, nil) {
		t.Fatal("expected change between slice and nil")
	}
}

func TestBasicAuthChanged(t *testing.T) {
	a := []basicAuthUserModel{{User: types.StringValue("admin"), Password: types.StringValue("p")}}
	b := []basicAuthUserModel{{User: types.StringValue("admin"), Password: types.StringValue("p")}}
	if basicAuthChanged(a, b) {
		t.Fatal("expected no change between equal slices")
	}
	b[0].Password = types.StringValue("q")
	if !basicAuthChanged(a, b) {
		t.Fatal("expected change when password rotates")
	}
}

func TestCreateRoute_SendsAllFields(t *testing.T) {
	appgwID := "appgw-1"
	routeID := "route-1"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "POST", Path: "/v1/app-gateways/" + appgwID + "/routes",
			Status: http.StatusCreated,
			BodyFn: func(t *testing.T, reqBody []byte) (int, any) {
				var got client.AppGWRouteCreateRequest
				if err := json.Unmarshal(reqBody, &got); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if got.ListenerID == "" {
					t.Errorf("missing listener_id in request body")
				}
				if got.TargetGroupID == "" {
					t.Errorf("missing target_group_id in request body")
				}
				if len(got.BasicAuthUsers) != 1 || got.BasicAuthUsers[0].User != "admin" {
					t.Errorf("missing/incorrect basic_auth_users: %+v", got.BasicAuthUsers)
				}
				// Wire-format guard: ensure we serialize the API contract key
				// `user` (not the legacy `username` which Pydantic silently
				// dropped and made the create 422).
				if !strings.Contains(string(reqBody), "\"user\":\"admin\"") {
					t.Errorf("expected \"user\":\"admin\" in wire payload, got: %s", string(reqBody))
				}
				if strings.Contains(string(reqBody), "\"username\"") {
					t.Errorf("legacy `username` key leaked into wire payload: %s", string(reqBody))
				}
				// Server doesn't echo passwords back — we do.
				return http.StatusCreated, map[string]any{
					"id":              routeID,
					"appgw_id":        appgwID,
					"listener_id":     got.ListenerID,
					"priority":        100,
					"path_match":      "/v1/",
					"path_match_type": "prefix",
					"header_matches":  []any{},
					"method_match":    []string{},
					"target_group_id": got.TargetGroupID,
					"allow_cidrs":     []string{},
					"deny_cidrs":      []string{},
					"request_headers": map[string]string{},
					"response_headers": map[string]string{},
					"cors_enabled":     false,
					"cors_origins":     []string{},
					"cors_methods":     []string{},
					"cors_credentials": false,
					"waf_preset":       "off",
					"strip_prefix":     false,
					"created_at":       "2026-05-15T10:00:00Z",
					"updated_at":       "2026-05-15T10:00:00Z",
				}
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	pathMatch := "/v1/"
	pathType := "prefix"
	created, err := c.CreateAppGWRoute(context.Background(), appgwID, client.AppGWRouteCreateRequest{
		ListenerID:    "listener-1",
		TargetGroupID: "tg-1",
		PathMatch:     &pathMatch,
		PathMatchType: &pathType,
		BasicAuthUsers: []client.AppGWBasicAuthUser{
			{User: "admin", Password: "hunter2"},
		},
	})
	if err != nil {
		t.Fatalf("CreateAppGWRoute: %v", err)
	}
	if created.ID != routeID {
		t.Errorf("expected id %q, got %q", routeID, created.ID)
	}
}

func TestCreateRoute_StripPrefixPropagates(t *testing.T) {
	appgwID := "appgw-1"
	routeID := "route-1"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "POST", Path: "/v1/app-gateways/" + appgwID + "/routes",
			Status: http.StatusCreated,
			BodyFn: func(t *testing.T, reqBody []byte) (int, any) {
				var got client.AppGWRouteCreateRequest
				if err := json.Unmarshal(reqBody, &got); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if got.StripPrefix == nil || *got.StripPrefix != true {
					t.Errorf("expected strip_prefix=true in request, got %+v", got.StripPrefix)
				}
				if !strings.Contains(string(reqBody), "\"strip_prefix\":true") {
					t.Errorf("expected \"strip_prefix\":true in wire payload, got: %s", string(reqBody))
				}
				return http.StatusCreated, map[string]any{
					"id":              routeID,
					"appgw_id":        appgwID,
					"listener_id":     got.ListenerID,
					"priority":        10,
					"path_match":      "/web-app",
					"path_match_type": "prefix",
					"header_matches":  []any{},
					"method_match":    []string{},
					"target_group_id": got.TargetGroupID,
					"allow_cidrs":     []string{},
					"deny_cidrs":      []string{},
					"request_headers": map[string]string{},
					"response_headers": map[string]string{},
					"cors_enabled":     false,
					"cors_origins":     []string{},
					"cors_methods":     []string{},
					"cors_credentials": false,
					"waf_preset":       "off",
					"strip_prefix":     true,
					"created_at":       "2026-05-21T10:00:00Z",
					"updated_at":       "2026-05-21T10:00:00Z",
				}
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	pathMatch := "/web-app"
	pathType := "prefix"
	strip := true
	created, err := c.CreateAppGWRoute(context.Background(), appgwID, client.AppGWRouteCreateRequest{
		ListenerID:    "listener-1",
		TargetGroupID: "tg-1",
		PathMatch:     &pathMatch,
		PathMatchType: &pathType,
		StripPrefix:   &strip,
	})
	if err != nil {
		t.Fatalf("CreateAppGWRoute: %v", err)
	}
	if !created.StripPrefix {
		t.Errorf("expected created.StripPrefix=true, got %v", created.StripPrefix)
	}
}

func TestUpdateRoute_StripPrefixPatch(t *testing.T) {
	appgwID := "appgw-1"
	routeID := "route-1"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "PATCH", Path: "/v1/app-gateways/" + appgwID + "/routes/" + routeID,
			Status: http.StatusOK,
			BodyFn: func(t *testing.T, reqBody []byte) (int, any) {
				bodyStr := string(reqBody)
				if !strings.Contains(bodyStr, "\"strip_prefix\":true") {
					t.Errorf("expected \"strip_prefix\":true in PATCH body, got: %s", bodyStr)
				}
				return http.StatusOK, map[string]any{
					"id":              routeID,
					"appgw_id":        appgwID,
					"listener_id":     "listener-1",
					"priority":        100,
					"path_match":      "/web-app",
					"path_match_type": "prefix",
					"header_matches":  []any{},
					"method_match":    []string{},
					"target_group_id": "tg-1",
					"allow_cidrs":     []string{},
					"deny_cidrs":      []string{},
					"request_headers": map[string]string{},
					"response_headers": map[string]string{},
					"cors_enabled":     false,
					"cors_origins":     []string{},
					"cors_methods":     []string{},
					"cors_credentials": false,
					"waf_preset":       "off",
					"strip_prefix":     true,
					"created_at":       "2026-05-21T10:00:00Z",
					"updated_at":       "2026-05-21T10:00:00Z",
				}
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	strip := true
	got, err := c.UpdateAppGWRoute(context.Background(), appgwID, routeID, client.AppGWRouteUpdateRequest{
		StripPrefix: &strip,
	})
	if err != nil {
		t.Fatalf("UpdateAppGWRoute: %v", err)
	}
	if !got.StripPrefix {
		t.Errorf("expected got.StripPrefix=true, got %v", got.StripPrefix)
	}
}

func TestUpdateRoute_PatchesPolicies(t *testing.T) {
	appgwID := "appgw-1"
	routeID := "route-1"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "PATCH", Path: "/v1/app-gateways/" + appgwID + "/routes/" + routeID,
			Status: http.StatusOK,
			BodyFn: func(t *testing.T, reqBody []byte) (int, any) {
				bodyStr := string(reqBody)
				if !strings.Contains(bodyStr, "\"rate_limit_per_sec\"") {
					t.Errorf("expected rate_limit_per_sec in PATCH body, got: %s", bodyStr)
				}
				if !strings.Contains(bodyStr, "\"waf_preset\"") {
					t.Errorf("expected waf_preset in PATCH body, got: %s", bodyStr)
				}
				return http.StatusOK, map[string]any{
					"id":              routeID,
					"appgw_id":        appgwID,
					"listener_id":     "listener-1",
					"priority":        50,
					"path_match_type": "prefix",
					"header_matches":  []any{},
					"method_match":    []string{},
					"target_group_id": "tg-1",
					"rate_limit_per_sec": 10,
					"allow_cidrs":     []string{},
					"deny_cidrs":      []string{},
					"request_headers": map[string]string{},
					"response_headers": map[string]string{},
					"cors_enabled":     false,
					"cors_origins":     []string{},
					"cors_methods":     []string{},
					"cors_credentials": false,
					"waf_preset":       "strict",
					"strip_prefix":     false,
					"created_at":       "2026-05-15T10:00:00Z",
					"updated_at":       "2026-05-15T10:00:00Z",
				}
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	rl := int64(10)
	waf := "strict"
	got, err := c.UpdateAppGWRoute(context.Background(), appgwID, routeID, client.AppGWRouteUpdateRequest{
		RateLimitPerSec: &rl,
		WAFPreset:       &waf,
	})
	if err != nil {
		t.Fatalf("UpdateAppGWRoute: %v", err)
	}
	if got.WAFPreset != "strict" {
		t.Errorf("expected waf_preset=strict, got %q", got.WAFPreset)
	}
}
