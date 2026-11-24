package ccks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
func TestWaitClusterSettled_PausedIsAnError(t *testing.T) {
	fast(t)
	op := map[string]any{"id": "op-2", "type": "patch", "origine": "automatique",
		"statut": "en_pause", "version_cible": "1.34.9"}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", op)},
	})
	defer srv.Close()
	_, err := WaitClusterSettled(context.Background(), client.New(srv.URL, "k", "test"), "cl-1", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("want a paused-upgrade error, got %v", err)
	}
}

// fakeCCKS is a stateful API: one cluster, one pool, upgrade operations that
// end after `polls` reads with success or failure. relaunch: the #2146
// behaviour (same target after a failure relaunches); otherwise the API
// answers 400/422 like before #2146.
type fakeCCKS struct {
	mu        sync.Mutex
	cl, pool  map[string]any
	ops       []map[string]any
	polls     int
	fail      bool // the next operation fails
	relaunch  bool
	effective bool // report k8s_version_effective (#2146)
	running   string
	poolRun   string
	posts     []string
	conflicts int // answer 409 to the next pool upgrades (another upgrade running)
}

func newFake() *fakeCCKS {
	f := &fakeCCKS{running: "1.34.8", poolRun: "1.34.8"}
	f.cl = cluster("active", nil)
	f.cl["k8s_version"] = "1.34.8"
	f.pool = pool("active")
	return f
}

func (f *fakeCCKS) view() map[string]any {
	if f.polls > 0 {
		f.polls--
		if f.polls == 0 {
			op := f.ops[len(f.ops)-1]
			f.cl["upgrade_en_cours"] = nil
			if f.fail {
				op["statut"], op["error_message"] = "echec", "rollout bloqué"
				f.cl["status"], f.cl["error_message"] = "error", "rollout bloqué"
				if op["type"] == "pool" {
					f.pool["status"], f.pool["error_message"] = "error", "rollout bloqué"
				}
				f.fail = false
			} else {
				op["statut"] = "terminee"
				f.cl["status"], f.cl["error_message"] = "active", nil
				if op["type"] == "pool" {
					f.pool["status"] = "active"
					f.poolRun = op["version_cible"].(string)
				} else {
					f.running = op["version_cible"].(string)
				}
			}
		}
	}
	if f.effective {
		f.cl["k8s_version_effective"] = f.running
		f.pool["k8s_version_effective"] = f.poolRun
	}
	return f.cl
}

func (f *fakeCCKS) start(typ, target string) map[string]any {
	op := map[string]any{"id": fmt.Sprintf("op-%d", len(f.ops)+1), "type": typ, "origine": "manuelle",
		"statut": "en_cours", "version_cible": target}
	f.ops = append(f.ops, op)
	f.cl["upgrade_en_cours"] = op
	f.cl["status"] = "updating"
	f.polls = 2
	return op
}

func (f *fakeCCKS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	reply := func(code int, v any) { w.WriteHeader(code); _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/k8s/clusters/cl-1":
		reply(200, f.view())
	case r.Method == "GET" && r.URL.Path == "/v1/k8s/clusters/cl-1/upgrades":
		hist := []map[string]any{}
		for i := len(f.ops) - 1; i >= 0; i-- {
			hist = append(hist, f.ops[i])
		}
		reply(200, hist)
	case r.Method == "GET" && r.URL.Path == "/v1/k8s/clusters/cl-1/node-pools/np-1":
		f.view()
		reply(200, f.pool)
	case r.Method == "POST" && r.URL.Path == "/v1/k8s/clusters/cl-1/upgrade-version":
		tgt := body["k8s_version"].(string)
		f.posts = append(f.posts, "cp "+tgt)
		cur := f.cl["k8s_version"].(string)
		if c, _ := Compare(tgt, cur); c <= 0 && !(f.relaunch && c == 0 && f.cl["status"] == "error") {
			reply(400, map[string]any{"detail": "La version cible doit être supérieure"})
			return
		}
		f.cl["k8s_version"] = tgt // target written at launch, like the API
		f.start("plan_de_controle", tgt)
		reply(200, f.cl)
	case r.Method == "POST" && r.URL.Path == "/v1/k8s/clusters/cl-1/node-pools/np-1/upgrade":
		tgt := body["k8s_version"].(string)
		f.posts = append(f.posts, "pool "+tgt)
		if f.conflicts > 0 {
			f.conflicts--
			f.start("patch", "1.34.9") // another upgrade starts meanwhile
			reply(409, map[string]any{"detail": "Une mise à niveau est déjà en cours"})
			return
		}
		cur := f.pool["k8s_version"].(string)
		if c, _ := Compare(tgt, cur); c <= 0 && !(f.relaunch && c == 0 && f.pool["status"] == "error") {
			reply(422, map[string]any{"detail": "La version cible du pool doit être supérieure"})
			return
		}
		f.pool["k8s_version"], f.pool["status"] = tgt, "updating"
		f.start("pool", tgt)
		reply(200, f.pool)
	default:
		reply(404, map[string]any{"detail": "nf"})
	}
}

func serve(t *testing.T, f *fakeCCKS) *client.Client {
	fast(t)
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	return client.New(ts.URL, "k", "test")
}

var cpT = ControlPlaneTimeouts{Settle: time.Minute, Upgrade: time.Minute}
var poolT = PoolTimeouts{Settle: time.Minute, Upgrade: time.Minute}

// Blocking point 2: the API writes the target at launch and keeps it after
// a failure. The failed apply reports it; the 2nd apply must not record the
// target as reached: it relaunches (#2146) and waits for the end.
func TestEnsureControlPlane_FailureThenSecondApplyRelaunches(t *testing.T) {
	for _, effective := range []bool{false, true} {
		t.Run(fmt.Sprintf("effective=%v", effective), func(t *testing.T) {
			f := newFake()
			f.fail, f.relaunch, f.effective = true, true, effective
			c := serve(t, f)
			_, _, err := EnsureControlPlane(context.Background(), c, "cl-1", "v1.35.0", cpT)
			if err == nil || !strings.Contains(err.Error(), "rollout bloqué") || !strings.Contains(err.Error(), "Apply again") {
				t.Fatalf("1st apply: want the failure and the hint, got %v", err)
			}
			// 2nd apply: k8s_version reads 1.35.0 (target) — must not be taken as done.
			final, res, err := EnsureControlPlane(context.Background(), c, "cl-1", "v1.35.0", cpT)
			if err != nil {
				t.Fatalf("2nd apply: %v", err)
			}
			if !res.Relaunched || len(f.posts) != 2 || final.Status != "active" {
				t.Fatalf("2nd apply must relaunch: res=%+v posts=%v status=%s", res, f.posts, final.Status)
			}
			if effective && ClusterRunning(final) != "1.35.0" {
				t.Fatalf("running = %s", ClusterRunning(final))
			}
		})
	}
}

// Before #2146 the API refuses to relaunch: a clear error, never a success
// that records the failed target.
func TestEnsureControlPlane_OldAPIRefusesRelaunch(t *testing.T) {
	f := newFake()
	f.fail = true
	c := serve(t, f)
	_, _, _ = EnsureControlPlane(context.Background(), c, "cl-1", "v1.35.0", cpT)
	_, _, err := EnsureControlPlane(context.Background(), c, "cl-1", "v1.35.0", cpT)
	if err == nil || !strings.Contains(err.Error(), "failed (rollout bloqué)") || !strings.Contains(err.Error(), "refused to relaunch") {
		t.Fatalf("want a clear refusal, got %v", err)
	}
}

// Point 5: a running automatic patch is waited for BEFORE comparing; when it
// reaches the target nothing is sent (no 400).
func TestEnsureControlPlane_WaitsThenCompares(t *testing.T) {
	f := newFake()
	f.effective = true
	f.cl["k8s_version"] = "1.34.9"
	f.start("patch", "1.34.9")
	c := serve(t, f)
	_, res, err := EnsureControlPlane(context.Background(), c, "cl-1", "v1.34.9", cpT)
	if err != nil || len(f.posts) != 0 || res.Relaunched {
		t.Fatalf("err=%v posts=%v", err, f.posts)
	}
	// Below the running version: a warning, nothing sent.
	_, res, err = EnsureControlPlane(context.Background(), c, "cl-1", "v1.34.8", cpT)
	if err != nil || res.Warning == "" || len(f.posts) != 0 {
		t.Fatalf("downgrade: err=%v warning=%q posts=%v", err, res.Warning, f.posts)
	}
}

// Pools: same failure / relaunch rule.
func TestEnsurePool_FailureThenRelaunch(t *testing.T) {
	f := newFake()
	f.fail, f.relaunch = true, true
	c := serve(t, f)
	if _, _, err := EnsurePool(context.Background(), c, "cl-1", "np-1", "v1.34.9", poolT); err == nil {
		t.Fatal("1st apply must fail")
	}
	p, res, err := EnsurePool(context.Background(), c, "cl-1", "np-1", "v1.34.9", poolT)
	if err != nil || !res.Relaunched || p.Status != "active" {
		t.Fatalf("2nd apply: err=%v res=%+v", err, res)
	}
	f2 := newFake()
	f2.fail = true
	c2 := serve(t, f2)
	_, _, _ = EnsurePool(context.Background(), c2, "cl-1", "np-1", "v1.34.9", poolT)
	if _, _, err := EnsurePool(context.Background(), c2, "cl-1", "np-1", "v1.34.9", poolT); err == nil ||
		!strings.Contains(err.Error(), "refused to relaunch") {
		t.Fatalf("old API: want a clear refusal, got %v", err)
	}
}

// Point 4: a 409 caused by ANOTHER upgrade running is waited for and not
// counted against the retry budget (several pools of one apply).
func TestEnsurePool_ConcurrentConflictsNotCounted(t *testing.T) {
	f := newFake()
	f.conflicts = upgradeConflictRetries + 2
	c := serve(t, f)
	p, _, err := EnsurePool(context.Background(), c, "cl-1", "np-1", "v1.34.9", poolT)
	if err != nil || p.Status != "active" {
		t.Fatalf("err=%v", err)
	}
	if len(f.posts) != upgradeConflictRetries+3 {
		t.Fatalf("posts = %v", f.posts)
	}
	for _, call := range f.posts {
		if !strings.HasPrefix(call, "pool ") {
			t.Fatalf("pool version must go through the upgrade route only: %v", f.posts)
		}
	}
}

// A 409 that lasts with NO other upgrade running (images guard) surfaces
// after a bounded number of tries.
func TestEnsurePool_PersistentConflictSurfaces(t *testing.T) {
	fast(t)
	var routes testutil.Routes
	for i := 0; i <= upgradeConflictRetries; i++ {
		routes = append(routes,
			testutil.Route{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)},
			testutil.Route{Method: "GET", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1", Body: pool("active")},
			testutil.Route{Method: "POST", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1/upgrade",
				Status: http.StatusConflict, Body: map[string]any{"detail": "images non préchargées"}},
			testutil.Route{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: cluster("active", nil)})
	}
	srv := testutil.NewTestServer(t, routes)
	defer srv.Close()
	_, _, err := EnsurePool(context.Background(), client.New(srv.URL, "k", "test"), "cl-1", "np-1", "1.34.9", poolT)
	if err == nil || !strings.Contains(err.Error(), "images non préchargées") {
		t.Fatalf("want the 409 detail, got %v", err)
	}
}

func TestWaitPoolActive_DeletingIsAnError(t *testing.T) {
	fast(t)
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1/node-pools/np-1", Body: pool("deleting")},
	})
	defer srv.Close()
	_, err := WaitPoolActive(context.Background(), client.New(srv.URL, "k", "test"), "cl-1", "np-1", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "deleted") {
		t.Fatalf("want an error at once, got %v", err)
	}
}

// Point 8 (+ follow-up 4): a plan change cancelled by the platform (plan
// reverted, reason in error_message) is an error; a plan not applied yet
// without a reason is watched longer; one that never comes times out.
func TestWatchPlanChange(t *testing.T) {
	fast(t)
	old := PlanSettlePolls
	PlanSettlePolls = 2
	t.Cleanup(func() { PlanSettlePolls = old })
	mk := func(plan string, msg any) map[string]any {
		b := cluster("active", nil)
		b["plan"], b["error_message"] = plan, msg
		return b
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		// 1. cancelled
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: mk("pro", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: mk("standard", "Passage au plan Pro annulé : bloc non extensible")},
		// 2. not applied yet (no reason) for 3 reads, then applied
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: mk("standard", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: mk("standard", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: mk("standard", nil)},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Body: mk("pro", nil)},
	})
	defer srv.Close()
	c := client.New(srv.URL, "k", "test")
	if _, err := WatchPlanChange(context.Background(), c, "cl-1", "pro", time.Minute); err == nil || !strings.Contains(err.Error(), "annulé") {
		t.Fatalf("want the cancellation, got %v", err)
	}
	if _, err := WatchPlanChange(context.Background(), c, "cl-1", "pro", time.Minute); err != nil {
		t.Fatalf("pending without a reason must be watched longer: %v", err)
	}
	if srv.PendingRoutes() != 0 {
		t.Fatalf("%d reads not done", srv.PendingRoutes())
	}
	// 3. never applied: timeout error.
	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mk("standard", nil))
	}))
	defer never.Close()
	if _, err := WatchPlanChange(context.Background(), client.New(never.URL, "k", "test"), "cl-1", "pro",
		20*time.Millisecond); err == nil || !strings.Contains(err.Error(), "still reports") {
		t.Fatalf("want a timeout, got %v", err)
	}
}

// Follow-up 2: an error state with an operation still listed as running
// fails at once (no wait until the timeout).
func TestErrorWithRunningOperationFailsAtOnce(t *testing.T) {
	f := newFake()
	f.cl["status"], f.cl["error_message"] = "error", "etcd injoignable"
	f.cl["upgrade_en_cours"] = map[string]any{"id": "op-x", "type": "patch", "origine": "automatique",
		"statut": "en_cours", "version_cible": "1.34.9"}
	c := serve(t, f)
	start := time.Now()
	if _, err := WaitClusterSettled(context.Background(), c, "cl-1", time.Hour); err == nil || !strings.Contains(err.Error(), "etcd injoignable") {
		t.Fatalf("settled: %v", err)
	}
	if _, err := WaitUpgradeDone(context.Background(), c, "cl-1", "op-x", time.Hour); err == nil || !strings.Contains(err.Error(), "etcd injoignable") {
		t.Fatalf("upgrade done: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("must not wait")
	}
}

// Follow-up 2: EnsurePool refuses a cluster in error (other than the
// failure of this pool) with a clear message and without posting; a pool in
// error that is not a failed upgrade towards the target is reported as such.
func TestEnsurePool_ClusterOrPoolInError(t *testing.T) {
	f := newFake()
	f.cl["status"], f.cl["error_message"] = "error", "plan de contrôle cassé"
	c := serve(t, f)
	if _, _, err := EnsurePool(context.Background(), c, "cl-1", "np-1", "v1.34.9", poolT); err == nil ||
		!strings.Contains(err.Error(), "plan de contrôle cassé") || len(f.posts) != 0 {
		t.Fatalf("cluster in error: err=%v posts=%v", err, f.posts)
	}
	f2 := newFake()
	f2.pool["status"], f2.pool["error_message"] = "error", "disque plein"
	c2 := serve(t, f2)
	if _, _, err := EnsurePool(context.Background(), c2, "cl-1", "np-1", "v1.34.9", poolT); err == nil ||
		!strings.Contains(err.Error(), "disque plein") || len(f2.posts) != 0 {
		t.Fatalf("pool in error: err=%v posts=%v", err, f2.posts)
	}
}

// Follow-up 5: with k8s_version_effective the history is not read.
func TestEnsureControlPlane_NoHistoryWithEffective(t *testing.T) {
	f := newFake()
	f.effective = true
	f.cl["k8s_version"] = "1.34.8"
	reads := 0
	c := serve(t, f)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/upgrades") {
			reads++
		}
		f.ServeHTTP(w, r)
	}))
	defer ts.Close()
	c = client.New(ts.URL, "k", "test")
	if _, _, err := EnsureControlPlane(context.Background(), c, "cl-1", "v1.34.8", cpT); err != nil || reads != 0 {
		t.Fatalf("err=%v history reads=%d", err, reads)
	}
	f.effective = false
	delete(f.cl, "k8s_version_effective")
	if _, _, err := EnsureControlPlane(context.Background(), c, "cl-1", "v1.34.8", cpT); err != nil || reads != 1 {
		t.Fatalf("without effective the history is read: err=%v reads=%d", err, reads)
	}
}

func TestTimeout(t *testing.T) {
	o := types.ObjectValueMust(TimeoutsAttrTypes("update"), map[string]attr.Value{"update": types.StringValue("2h")})
	if Timeout(o, "update", time.Minute) != 2*time.Hour {
		t.Fatal("configured value")
	}
	if Timeout(types.ObjectNull(TimeoutsAttrTypes("update")), "update", time.Minute) != time.Minute {
		t.Fatal("default")
	}
}
