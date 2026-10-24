package appgwroute

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCacheUpdateSendsOffFalseAndEmptyCookieList(t *testing.T) {
	ctx := context.Background()
	route := client.AppGWRoute{ID: "route", AppGWID: "gateway", ListenerID: "listener", TargetGroupID: "tg",
		CacheMode: "force", CacheDefaultTTLS: 90, CacheMaxTTLS: 600, CacheDebugHeaders: true, CacheBypassCookies: []string{"session"}}
	var state routeResourceModel
	applyToModel(ctx, &route, &state)
	plan := state
	plan.CacheMode = types.StringValue("off")
	plan.CacheDebugHeaders = types.BoolValue(false)
	plan.CacheBypassCookies = types.SetValueMust(types.StringType, nil)
	route.CacheMode, route.CacheDebugHeaders, route.CacheBypassCookies = "off", false, []string{}
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/app-gateways/gateway/routes/route", Status: http.StatusOK, Body: route},
	})
	defer server.Close()
	r := &routeResource{client: client.New(server.URL, "test", "test")}
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	old := tfsdk.State{Schema: schema.Schema}
	next := tfsdk.State{Schema: schema.Schema}
	if diagnostics := old.Set(ctx, &state); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if diagnostics := next.Set(ctx, &plan); diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	response := resource.UpdateResponse{State: old}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Raw: next.Raw, Schema: schema.Schema}, State: old}, &response)
	if response.Diagnostics.HasError() {
		t.Fatal(response.Diagnostics)
	}
	calls := server.Calls()
	if len(calls) != 1 {
		t.Fatal(calls)
	}
	var payload map[string]any
	if err := json.Unmarshal(calls[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["cache_mode"] != "off" || payload["cache_debug_headers"] != false {
		t.Fatalf("missing off/false: %v", payload)
	}
	cookies, ok := payload["cache_bypass_cookies"].([]any)
	if !ok || len(cookies) != 0 {
		t.Fatalf("missing explicit empty list: %v", payload)
	}
}

func TestCacheCookieOrderIsNotPartOfState(t *testing.T) {
	ctx := context.Background()
	var first, second routeResourceModel
	applyToModel(ctx, &client.AppGWRoute{CacheBypassCookies: []string{"session", "auth"}}, &first)
	applyToModel(ctx, &client.AppGWRoute{CacheBypassCookies: []string{"auth", "session"}}, &second)
	if !first.CacheBypassCookies.Equal(second.CacheBypassCookies) {
		t.Fatal("API canonicalization must not change the planned cookie set")
	}
}
