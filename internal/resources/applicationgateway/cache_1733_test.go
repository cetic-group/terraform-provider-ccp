package applicationgateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCachePlanChangeUsesServerProfileWithoutSendingDimensions(t *testing.T) {
	for _, tc := range []struct {
		name, plan   string
		size, object int64
	}{
		{"downgrade", "appgw-small", 64, 512},
		{"upgrade", "appgw-large", 1024, 512},
		{"server-defined profile", "appgw-custom", 192, 384},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "cache-gateway"
			response := fixtureActive(id, map[string]any{"plan": tc.plan, "cache_enabled": true,
				"cache_size_mb": tc.size, "cache_max_object_kb": tc.object,
				"cache_effective_size_mb": tc.size, "cache_size_limit_mb": tc.size})
			server := testutil.NewTestServer(t, testutil.Routes{
				{Method: "PATCH", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: response},
				{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: response},
				{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: response},
			})
			defer server.Close()
			state := gwModel(id, types.StringNull())
			state.CacheEnabled = types.BoolValue(true)
			state.CacheSizeMB = types.Int64Value(256)
			state.CacheMaxObjectKB = types.Int64Value(512)
			plan := state
			plan.Plan = types.StringValue(tc.plan)
			plan.CacheSizeMB = types.Int64Unknown()
			plan.CacheMaxObjectKB = types.Int64Unknown()
			out, result := runGwUpdate(t, server, state, plan)
			if result.Diagnostics.HasError() {
				t.Fatal(result.Diagnostics)
			}
			patches := 0
			for _, call := range server.Calls() {
				if call.Method != "PATCH" {
					continue
				}
				patches++
				var payload map[string]any
				if err := json.Unmarshal(call.Body, &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 1 || payload["plan"] != tc.plan {
					t.Fatalf("unexpected patch: %s", call.Body)
				}
			}
			if patches != 1 {
				t.Fatalf("expected one patch, got %d", patches)
			}
			if out.CacheSizeMB.ValueInt64() != tc.size || out.CacheMaxObjectKB.ValueInt64() != tc.object || out.CacheEffectiveSizeMB.ValueInt64() != tc.size {
				t.Fatalf("server profile not stored: %+v", out)
			}
			// A subsequent apply over the refreshed state must not send another PATCH.
			before := len(server.Calls())
			if _, result = runGwUpdate(t, server, out, out); result.Diagnostics.HasError() {
				t.Fatal(result.Diagnostics)
			}
			for _, call := range server.Calls()[before:] {
				if call.Method == "PATCH" {
					t.Fatalf("perpetual drift: %s", call.Body)
				}
			}
		})
	}
}

func TestCacheCreateNeverSendsDimensions(t *testing.T) {
	plan := planModel(types.StringUnknown())
	plan.CacheEnabled = types.BoolValue(true)
	// A stale caller/state cannot accidentally forward writable dimensions.
	plan.CacheSizeMB = types.Int64Value(99)
	plan.CacheMaxObjectKB = types.Int64Value(123)
	sent, _ := runCreate(t, plan, "10.0.1.254")
	if sent["cache_enabled"] != true {
		t.Fatalf("cache enablement missing: %v", sent)
	}
	for _, key := range []string{"cache_size_mb", "cache_max_object_kb"} {
		if _, exists := sent[key]; exists {
			t.Fatalf("read-only %s sent: %v", key, sent)
		}
	}
}
