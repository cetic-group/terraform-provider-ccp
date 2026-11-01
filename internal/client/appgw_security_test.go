package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestAccessPolicyCapabilityRequiredBeforeWrite(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "GET", Path: "/v1/platform/capabilities", Status: 200, Body: map[string]any{}}})
	defer srv.Close()
	_, err := New(srv.URL, "test", "test").CreateApplicationGateway(context.Background(), ApplicationGatewayCreateRequest{L7Policy: &AppGWL7Policy{}})
	if err == nil || len(srv.Calls()) != 1 {
		t.Fatal("unsupported server must not receive a mutation")
	}
}
func TestAccessPolicyResetCarriesFreshRevision(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/platform/capabilities", Status: 200, Body: map[string]any{"appgw_security_policy_versions": []int{1}}},
		{Method: "GET", Path: "/v1/app-gateways/g", Status: 200, Body: map[string]any{"id": "g", "access_policy_generation": 0}},
		{Method: "PATCH", Path: "/v1/app-gateways/g/listeners/l", Status: 200, Body: map[string]any{"id": "l", "l7_policy": map[string]any{}, "security_revision": 1}},
	})
	defer srv.Close()
	_, err := New(srv.URL, "test", "test").UpdateAppGWListener(context.Background(), "g", "l", AppGWListenerUpdateRequest{L7Policy: &AppGWL7Policy{}})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]json.RawMessage
	_ = json.Unmarshal(srv.Calls()[2].Body, &sent)
	if string(sent["l7_policy"]) != "{}" || string(sent["security_revision"]) != "0" {
		t.Fatalf("invalid reset payload: %s", srv.Calls()[2].Body)
	}
}
func TestPolicyWritesSerializedForSiblingResources(t *testing.T) {
	var mu sync.Mutex
	generation := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/platform/capabilities" {
			json.NewEncoder(w).Encode(map[string]any{"appgw_security_policy_versions": []int{1}})
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"id": "g", "access_policy_generation": generation})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if int64(body["security_revision"].(float64)) != generation {
			w.WriteHeader(409)
			return
		}
		generation++
		json.NewEncoder(w).Encode(map[string]any{"id": "l", "l7_policy": body["l7_policy"], "security_revision": generation})
	}))
	defer srv.Close()
	c := New(srv.URL, "test", "test")
	errs := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		go func(id string) {
			_, err := c.UpdateAppGWListener(context.Background(), "g", id, AppGWListenerUpdateRequest{L7Policy: &AppGWL7Policy{}})
			errs <- err
		}(id)
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if generation != 2 {
		t.Fatal("both sibling changes must be accepted")
	}
}
func TestPolicyConvergenceRequiresAllExpectedReplicasAndRevocation(t *testing.T) {
	for _, scenario := range []string{"partial", "old-generation", "not-revoked", "missing-secondary", "applied"} {
		t.Run(scenario, func(t *testing.T) {
			status := map[string]any{"generation": 4, "state": "applied", "replicas": map[string]any{"primary": map[string]any{"generation": 4, "state": "applied", "revocation_complete": true}, "secondary": map[string]any{"generation": 4, "state": "applied", "revocation_complete": true}}}
			replicas := status["replicas"].(map[string]any)
			switch scenario {
			case "partial":
				status["state"] = "partial"
			case "old-generation":
				status["generation"] = 3
			case "not-revoked":
				replicas["secondary"].(map[string]any)["revocation_complete"] = false
			case "missing-secondary":
				delete(replicas, "secondary")
			}
			srv := testutil.NewTestServer(t, testutil.Routes{{Method: "GET", Path: "/v1/app-gateways/g", Status: 200, Body: map[string]any{"id": "g", "status": "active", "replica_count": 2, "access_policy_generation": 4, "access_policy_status": status}}})
			defer srv.Close()
			_, err := New(srv.URL, "test", "test").WaitAppGWAccessPolicy(context.Background(), "g", 3, 10*time.Millisecond)
			if (err == nil) != (scenario == "applied") {
				t.Fatalf("scenario %s: %v", scenario, err)
			}
		})
	}
}
func TestPolicyErrorsNeverEchoHeaderValues(t *testing.T) {
	for _, code := range []int{403, 409, 422, 500} {
		err := AppGWPolicyError(&APIError{StatusCode: code, Detail: "customer-token-sensitive"})
		if strings.Contains(err.Error(), "customer-token-sensitive") {
			t.Fatal("raw values leaked")
		}
	}
}

func appliedPolicyGateway(generation int64) map[string]any {
	return map[string]any{"id": "g", "status": "active", "replica_count": 1, "access_policy_generation": generation, "access_policy_status": map[string]any{"generation": generation, "state": "applied", "replicas": map[string]any{"primary": map[string]any{"generation": generation, "state": "applied", "revocation_complete": true}}}}
}
func TestPolicyConvergenceToleratesLaggingRead(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/app-gateways/g", Body: appliedPolicyGateway(4)},
		{Method: "GET", Path: "/v1/app-gateways/g", Body: appliedPolicyGateway(5)},
	})
	defer srv.Close()
	gw, err := New(srv.URL, "test", "test").WaitAppGWAccessPolicy(context.Background(), "g", 5, 3*time.Second)
	if err != nil || gw == nil || *gw.AccessPolicyGeneration != 5 {
		t.Fatalf("lagging read did not recover: %v", err)
	}
}
func TestProtectedChildDeleteWaitsForAppliedAbsence(t *testing.T) {
	for _, collection := range []string{"listeners", "routes"} {
		t.Run(collection, func(t *testing.T) {
			reads := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					w.Header().Set("X-AppGW-Security-Revision", "6")
					w.WriteHeader(204)
					return
				}
				reads++
				gw := appliedPolicyGateway(6)
				if reads == 1 {
					gw[collection] = []any{map[string]any{"id": "child"}}
				} else {
					gw[collection] = []any{map[string]any{"id": "sibling"}}
				}
				_ = json.NewEncoder(w).Encode(gw)
			}))
			defer srv.Close()
			c := New(srv.URL, "test", "test")
			var err error
			if collection == "listeners" {
				err = c.DeleteAppGWListener(context.Background(), "g", "child", 5)
			} else {
				err = c.DeleteAppGWRoute(context.Background(), "g", "child", 5)
			}
			if err != nil || reads != 2 {
				t.Fatalf("deletion did not wait for exact child absence: reads=%d err=%v", reads, err)
			}
		})
	}
}
func TestProtectedDeleteRetry404StillChecksApplication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(appliedPolicyGateway(6))
	}))
	defer srv.Close()
	if err := New(srv.URL, "test", "test").DeleteAppGWRoute(context.Background(), "g", "gone", 5); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyFeatureDisabledAllowsOnlyExplicitReset(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-policy", true: "reset"}[reset], func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					_ = json.NewEncoder(w).Encode(map[string]any{"appgw_security_policy_versions": []int{1}, "appgw_security_policies_enabled": false})
					return
				}
				writes++
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "g", "l7_policy": map[string]any{}})
			}))
			defer srv.Close()
			policy := &AppGWL7Policy{Enabled: !reset}
			_, err := New(srv.URL, "test", "test").CreateApplicationGateway(context.Background(), ApplicationGatewayCreateRequest{L7Policy: policy})
			if reset && (err != nil || writes != 1) {
				t.Fatalf("reset refused: %v", err)
			}
			if !reset && (err == nil || writes != 0) {
				t.Fatal("disabled feature accepted a new policy")
			}
		})
	}
}
