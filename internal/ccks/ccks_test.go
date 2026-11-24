package ccks

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strp(s string) *string { return &s }

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v1.34.8", "1.34.8", 0, true},
		{"1.34.9", "v1.34.8", 1, true},
		{"1.33.10", "1.34.0", -1, true},
		{"1.34.10", "1.34.9", 1, true}, // numeric, not lexical
		{"1.34", "1.34.0", 0, false},
		{"latest", "1.34.0", 0, false},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Compare(%q, %q) = %d,%v want %d,%v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
	if IsAbove("1.34", "1.33.0") {
		t.Error("an unparsable target must not be reported above (the API decides)")
	}
}

// Floor: the desired version stays in the state while the platform runs it
// or higher (automatic patch, pinned pool, console upgrade) — no perpetual
// diff, no downgrade attempt.
func TestFloor(t *testing.T) {
	cases := []struct {
		name   string
		prior  types.String
		actual *string
		want   types.String
	}{
		{"same version, v prefix kept", types.StringValue("v1.34.8"), strp("1.34.8"), types.StringValue("v1.34.8")},
		{"automatic patch above", types.StringValue("v1.34.8"), strp("1.34.9"), types.StringValue("v1.34.8")},
		{"console minor upgrade above", types.StringValue("v1.33.4"), strp("1.34.0"), types.StringValue("v1.33.4")},
		{"actual below: actual wins", types.StringValue("v1.34.8"), strp("1.34.2"), types.StringValue("1.34.2")},
		{"no prior (import)", types.StringNull(), strp("1.34.8"), types.StringValue("1.34.8")},
		{"unknown prior (computed in plan)", types.StringUnknown(), strp("1.34.8"), types.StringValue("1.34.8")},
		{"pool inherits: prior kept", types.StringValue("v1.34.8"), nil, types.StringValue("v1.34.8")},
		{"pool inherits, unknown prior", types.StringUnknown(), nil, types.StringNull()},
		{"unparsable prior", types.StringValue("latest"), strp("1.34.8"), types.StringValue("1.34.8")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Floor(c.prior, c.actual); !got.Equal(c.want) {
				t.Fatalf("Floor = %s, want %s", got, c.want)
			}
		})
	}
}

func cluster(status string, op map[string]any) map[string]any {
	b := map[string]any{
		"id": "cl-1", "name": "c", "region": "RNN", "k8s_version": "1.34.8",
		"status": status, "tags": []string{},
		"created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z",
	}
	if op != nil {
		b["upgrade_en_cours"] = op
	}
	return b
}

func pool(status string) map[string]any {
	return map[string]any{
		"id": "np-1", "cluster_id": "cl-1", "name": "default", "plan": "small", "replicas": 1,
		"labels": map[string]string{}, "taints": []any{}, "k8s_version": "1.34.8", "status": status,
		"created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z",
	}
}

func fast(t *testing.T) {
	old := PollInterval
	PollInterval = time.Millisecond
	t.Cleanup(func() { PollInterval = old })
}

// WaitUpgradeDone waits for `updating` → `active` and for the operation to
// leave `upgrade_en_cours`, then reads its outcome: a failed operation is an
// error even though the cluster is active again.
func TestWaitUpgradeDone_Failure(t *testing.T) {
	fast(t)
	op := map[string]any{"id": "op-1", "type": "plan_de_controle", "origine": "manuelle",
		"statut": "en_cours", "version_cible": "1.35.0"}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("updating", op)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1/upgrades", Body: []map[string]any{
			{"id": "op-1", "type": "plan_de_controle", "origine": "manuelle", "statut": "echec",
				"version_cible": "1.35.0", "error_message": "rollout bloqué"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "k", "test")
	_, err := WaitUpgradeDone(context.Background(), c, "cl-1", "op-1", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "rollout bloqué") {
		t.Fatalf("want the failure of the operation, got %v", err)
	}
	if srv.PendingRoutes() != 0 {
		t.Fatalf("%d fixtures not consumed", srv.PendingRoutes())
	}
}

// A paused automatic upgrade (waiting for its window, maybe days) is not
// waited for.
func TestWaitClusterIdle_PausedIsAnError(t *testing.T) {
	fast(t)
	op := map[string]any{"id": "op-2", "type": "patch", "origine": "automatique",
		"statut": "en_pause", "version_cible": "1.34.9"}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", op)},
	})
	defer srv.Close()
	_, err := WaitClusterIdle(context.Background(), client.New(srv.URL, "k", "test"), "cl-1", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("want a paused-upgrade error, got %v", err)
	}
}

// UpgradePool: waits for an idle cluster, calls the pool upgrade route
// (never the PATCH), retries a 409 raised by a concurrent upgrade, then waits
// for the operation and the pool.
func TestUpgradePool_RetriesConflictThenWaits(t *testing.T) {
	fast(t)
	op := map[string]any{"id": "op-3", "type": "pool", "origine": "manuelle",
		"statut": "en_cours", "version_cible": "1.34.9"}
	srv := testutil.NewTestServer(t, testutil.Routes{
		// attempt 1: idle, pool active, 409 (another pool started meanwhile)
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1", Body: pool("active")},
		{Method: "POST", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1/upgrade", Status: http.StatusConflict,
			Body: map[string]any{"detail": "Une mise à niveau est déjà en cours"}},
		// attempt 2
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1", Body: pool("active")},
		{Method: "POST", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1/upgrade", Body: pool("updating")},
		// operation id, then its end
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("updating", op)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("updating", op)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1/upgrades", Body: []map[string]any{
			{"id": "op-3", "type": "pool", "origine": "manuelle", "statut": "terminee", "version_cible": "1.34.9"},
		}},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1", Body: pool("active")},
	})
	defer srv.Close()
	got, err := UpgradePool(context.Background(), client.New(srv.URL, "k", "test"), "cl-1", "np-1", "1.34.9", time.Minute)
	if err != nil {
		t.Fatalf("UpgradePool: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("pool status %s", got.Status)
	}
	var posts int
	for _, call := range srv.Calls() {
		if call.Method == "PATCH" {
			t.Fatalf("the pool version must not go through PATCH any more: %s", call.Path)
		}
		if call.Method == "POST" {
			posts++
			var body map[string]any
			_ = json.Unmarshal(call.Body, &body)
			if body["k8s_version"] != "1.34.9" {
				t.Fatalf("upgrade body = %s", call.Body)
			}
		}
	}
	if posts != 2 || srv.PendingRoutes() != 0 {
		t.Fatalf("posts=%d pending=%d", posts, srv.PendingRoutes())
	}
}

// A 409 that lasts (images guard) surfaces after a bounded number of tries.
func TestUpgradePool_PersistentConflictSurfaces(t *testing.T) {
	fast(t)
	var routes testutil.Routes
	for i := 0; i <= upgradeConflictRetries; i++ {
		routes = append(routes,
			testutil.Route{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)},
			testutil.Route{Method: "GET", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1", Body: pool("active")},
			testutil.Route{Method: "POST", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1/upgrade",
				Status: http.StatusConflict, Body: map[string]any{"detail": "images non préchargées"}})
	}
	srv := testutil.NewTestServer(t, routes)
	defer srv.Close()
	_, err := UpgradePool(context.Background(), client.New(srv.URL, "k", "test"), "cl-1", "np-1", "1.34.9", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "images non préchargées") {
		t.Fatalf("want the 409 detail, got %v", err)
	}
}
