package applicationgateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCacheDowngradePreservesRequestedBudgetAndSendsPlan(t *testing.T) {
	id := "cache-gateway"
	response := fixtureActive(id, map[string]any{"plan": "appgw-small", "cache_enabled": false,
		"cache_size_mb": 256, "cache_max_object_kb": 512, "cache_effective_size_mb": 64, "cache_size_limit_mb": 64})
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: response},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: response},
	})
	defer server.Close()
	state := gwModel(id, types.StringNull())
	state.CacheEnabled = types.BoolValue(true)
	state.CacheSizeMB = types.Int64Value(256)
	state.CacheMaxObjectKB = types.Int64Value(512)
	plan := state
	plan.Plan = types.StringValue("appgw-small")
	plan.CacheEnabled = types.BoolValue(false)
	out, result := runGwUpdate(t, server, state, plan)
	if result.Diagnostics.HasError() {
		t.Fatal(result.Diagnostics)
	}
	for _, call := range server.Calls() {
		if call.Method != "PATCH" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(call.Body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["cache_enabled"] != false || payload["plan"] != "appgw-small" {
			t.Fatalf("wrong patch: %s", call.Body)
		}
		if _, ok := payload["cache_size_mb"]; ok {
			t.Fatalf("unchanged requested budget must be omitted: %s", call.Body)
		}
	}
	if out.CacheSizeMB.ValueInt64() != 256 || out.CacheEffectiveSizeMB.ValueInt64() != 64 {
		t.Fatalf("requested/effective budget conflated: %+v", out)
	}
}
