package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/ccks"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// cetic-cloud-platform#2143 — `ccp_k8s_cluster` à travers le vrai serveur du
// provider contre une API simulée au contrat #2143, en partant d'un état écrit
// par le provider antérieur (pas de `plan`, `maintenance`, `oidc`…).
//
// Ce que le test prouve : une configuration antérieure (`tier = "prod"`, sans
// `os_image`) ne planifie rien après la migration de l'API, même quand la
// plateforme a patché le cluster (version et template réécrits) ; passer à
// `plan` ne change rien ; standard → pro est en place ; standard → starter
// remplace ; un changement de tags ne remplace pas le cluster.

type fakeCCKSAPI struct {
	mu      sync.Mutex
	cluster map[string]any
	maint   map[string]any
	calls   []string
	// upgrades: failNext makes the next control-plane upgrade fail; relaunch
	// is the #2146 behaviour (same target after a failure relaunches).
	failNext, relaunch bool
	ops                []map[string]any
	pending            int
	oidcStatus         int // answer of PUT /oidc (0 = 200)
}

func newFakeCCKSAPI() *fakeCCKSAPI {
	return &fakeCCKSAPI{
		cluster: map[string]any{
			"id": "cl-1", "name": "prod", "display_name": nil, "region": "RNN",
			// Patché par la plateforme : version et template réécrits.
			"k8s_version": "1.34.9", "os_template_key": "kube-v1-34-9", "os_image": "flatcar",
			"vpc_id": "vpc-1", "vnet_id": "vnet-1", "pod_cidr": "10.244.0.0/16", "service_cidr": "10.96.0.0/12",
			"autoscaler_scale_down_delay_after_add": "10m", "autoscaler_scale_down_unneeded_time": "10m",
			"ingress_controller_enabled": true, "ingress_controller_scope": "internal",
			"ingress_controller_class": "incluster", "ingress_internal_ip": "10.0.0.50",
			"plan": "standard", "tier": "prod", "etcd_volume_gb": 5, "node_ip_block_size": 176,
			"proxy_vip_vnet": "10.0.0.250", "oidc": nil, "patch_disponible": nil, "upgrade_en_cours": nil,
			"status": "active", "tags": []string{}, "created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z",
		},
		maint: map[string]any{
			"auto_patch": true, "auto_mineure": false, "auto_majeure": false,
			"fenetre":   map[string]any{"jours": []int{2, 3, 4}, "heure_debut": "02:00", "duree_heures": 4, "fuseau": "Europe/Paris"},
			"dates_gel": []any{}, "personnalise": false,
		},
	}
}

func (f *fakeCCKSAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, r.Method+" "+r.URL.Path+" "+string(body))
	w.Header().Set("Content-Type", "application/json")
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	if f.pending > 0 && r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1" {
		f.pending--
		if f.pending == 0 {
			op := f.ops[len(f.ops)-1]
			f.cluster["upgrade_en_cours"] = nil
			if f.failNext {
				f.failNext = false
				op["statut"], op["error_message"] = "echec", "rollout bloqué"
				f.cluster["status"], f.cluster["error_message"] = "error", "rollout bloqué"
			} else {
				op["statut"] = "terminee"
				f.cluster["status"], f.cluster["error_message"] = "active", nil
				f.cluster["k8s_version_effective"] = op["version_cible"]
			}
		}
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1":
		_ = json.NewEncoder(w).Encode(f.cluster)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1/maintenance":
		_ = json.NewEncoder(w).Encode(f.maint)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1/upgrades":
		hist := []map[string]any{}
		for i := len(f.ops) - 1; i >= 0; i-- {
			hist = append(hist, f.ops[i])
		}
		_ = json.NewEncoder(w).Encode(hist)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1/node-pools":
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "np-2", "cluster_id": "cl-1", "name": "extra", "plan": "medium", "replicas": 1, "disk_gb": 20,
				"labels": map[string]string{}, "taints": []any{}, "status": "active",
				"created_at": "2026-11-02T00:00:00Z", "updated_at": "2026-11-02T00:00:00Z"},
			{"id": "np-1", "cluster_id": "cl-1", "name": "default", "plan": "small", "replicas": 2, "disk_gb": 20,
				"k8s_version": "1.34.8", "labels": map[string]string{}, "taints": []any{}, "status": "active",
				"created_at": "2026-11-01T00:00:00Z", "updated_at": "2026-11-01T00:00:00Z"},
		})
	case r.Method == http.MethodPatch && r.URL.Path == "/v1/k8s/clusters/cl-1":
		for k, v := range req {
			f.cluster[k] = v
		}
		_ = json.NewEncoder(w).Encode(f.cluster)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/k8s/clusters":
		f.cluster["k8s_version"], f.cluster["os_template_key"] = req["k8s_version"], req["os_template_key"]
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.cluster)
	case r.Method == http.MethodPut && r.URL.Path == "/v1/k8s/clusters/cl-1/oidc":
		if f.oidcStatus != 0 {
			w.WriteHeader(f.oidcStatus)
			_, _ = w.Write([]byte(`{"detail":"issuer injoignable"}`))
			return
		}
		f.cluster["oidc"] = req
		_ = json.NewEncoder(w).Encode(f.cluster)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/k8s/clusters/cl-1/upgrade-version":
		tgt := req["k8s_version"].(string)
		relaunch := f.relaunch && f.cluster["status"] == "error" && tgt == f.cluster["k8s_version"]
		if tgt == f.cluster["k8s_version"] && !relaunch {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"La version cible doit être supérieure à la version actuelle"}`))
			return
		}
		op := map[string]any{"id": "op-" + tgt + string(rune('a'+len(f.ops))), "type": "plan_de_controle",
			"origine": "manuelle", "statut": "en_cours", "version_cible": tgt}
		f.ops = append(f.ops, op)
		// Like the API: the TARGET is written at launch, kept after a failure.
		f.cluster["k8s_version"], f.cluster["os_template_key"] = tgt, "kube-v"+strings.ReplaceAll(tgt, ".", "-")
		f.cluster["status"], f.cluster["upgrade_en_cours"] = "updating", op
		f.pending = 2
		_ = json.NewEncoder(w).Encode(f.cluster)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
	}
}

type ccksHarness struct {
	t   *testing.T
	ctx context.Context
	srv tfprotov6.ProviderServer
	typ tftypes.Object
}

func newCCKSHarness(t *testing.T, url string) *ccksHarness {
	t.Helper()
	ctx := context.Background()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	pt := sch.Provider.ValueType().(tftypes.Object)
	pv := map[string]tftypes.Value{}
	for k, at := range pt.AttributeTypes {
		pv[k] = tftypes.NewValue(at, nil)
	}
	pv["endpoint"] = tftypes.NewValue(tftypes.String, url)
	pv["api_key"] = tftypes.NewValue(tftypes.String, "ccp_test_abcdefghijklmnopqrstuvwxyz")
	cfg, err := tfprotov6.NewDynamicValue(pt, tftypes.NewValue(pt, pv))
	if err != nil {
		t.Fatal(err)
	}
	cr, err := srv.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{Config: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	h := &ccksHarness{t: t, ctx: ctx, srv: srv, typ: sch.ResourceSchemas["ccp_k8s_cluster"].ValueType().(tftypes.Object)}
	h.fail("configure", cr.Diagnostics)
	return h
}

func (h *ccksHarness) fail(step string, diags []*tfprotov6.Diagnostic) {
	h.t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			h.t.Fatalf("%s: %s — %s", step, d.Summary, d.Detail)
		}
	}
}

func (h *ccksHarness) enc(v tftypes.Value) *tfprotov6.DynamicValue {
	h.t.Helper()
	d, err := tfprotov6.NewDynamicValue(h.typ, v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func (h *ccksHarness) dec(d *tfprotov6.DynamicValue) tftypes.Value {
	h.t.Helper()
	v, err := d.Unmarshal(h.typ)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func (h *ccksHarness) obj(set map[string]tftypes.Value) tftypes.Value {
	m := map[string]tftypes.Value{}
	for k, at := range h.typ.AttributeTypes {
		m[k] = tftypes.NewValue(at, nil)
	}
	for k, v := range set {
		m[k] = v
	}
	return tftypes.NewValue(h.typ, m)
}

func (h *ccksHarness) initialPool() tftypes.Value {
	pt := h.typ.AttributeTypes["initial_pool"].(tftypes.Object)
	m := map[string]tftypes.Value{}
	for k, at := range pt.AttributeTypes {
		m[k] = tftypes.NewValue(at, nil)
	}
	m["name"] = tftypes.NewValue(tftypes.String, "default")
	m["plan"] = tftypes.NewValue(tftypes.String, "small")
	m["replicas"] = tftypes.NewValue(tftypes.Number, 2)
	return tftypes.NewValue(pt, m)
}

// config: a configuration written before #2143, plus overrides.
func (h *ccksHarness) config(set map[string]tftypes.Value) tftypes.Value {
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	m := map[string]tftypes.Value{
		"name": s("prod"), "region": s("RNN"), "vpc_id": s("vpc-1"), "vnet_id": s("vnet-1"),
		"k8s_version": s("v1.34.8"), "os_template_key": s("kube-v1-34-8"), "tier": s("prod"),
		"initial_pool": h.initialPool(),
	}
	for k, v := range set {
		m[k] = v
	}
	return h.obj(m)
}

// legacyState: what the provider before #2143 wrote (no plan, maintenance,
// oidc, etcd_volume_gb, node_ip_block_size, current_k8s_version).
func (h *ccksHarness) legacyState() tftypes.Value {
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	return h.obj(map[string]tftypes.Value{
		"id": s("cl-1"), "name": s("prod"), "region": s("RNN"), "vpc_id": s("vpc-1"), "vnet_id": s("vnet-1"),
		"k8s_version": s("v1.34.8"), "os_template_key": s("kube-v1-34-8"), "os_image": s("flatcar"),
		"pod_cidr": s("10.244.0.0/16"), "service_cidr": s("10.96.0.0/12"),
		"autoscaler_scale_down_delay_after_add": s("10m"), "autoscaler_scale_down_unneeded_time": s("10m"),
		"ingress_controller_enabled": tftypes.NewValue(tftypes.Bool, true),
		"ingress_controller_scope":   s("internal"), "ingress_controller_class": s("incluster"),
		"ingress_internal_ip": s("10.0.0.50"), "tier": s("prod"), "proxy_vip_vnet": s("10.0.0.250"),
		"status": s("active"), "tags": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{}),
		"created_at": s("2026-11-01T00:00:00Z"), "initial_pool": h.initialPool(),
	})
}

func (h *ccksHarness) read(state tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: "ccp_k8s_cluster", CurrentState: h.enc(state)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("read", r.Diagnostics)
	return h.dec(r.NewState)
}

// proposed mimics Terraform core at the top level: config values, prior
// state where the configuration leaves a (computed) attribute null.
func (h *ccksHarness) proposed(prior, cfg tftypes.Value) tftypes.Value {
	if prior.IsNull() {
		return cfg
	}
	var c, p map[string]tftypes.Value
	_ = cfg.As(&c)
	_ = prior.As(&p)
	out := map[string]tftypes.Value{}
	for k, v := range c {
		if v.IsNull() {
			out[k] = p[k]
		} else {
			out[k] = v
		}
	}
	return tftypes.NewValue(h.typ, out)
}

func (h *ccksHarness) plan(prior, cfg tftypes.Value) (tftypes.Value, *tfprotov6.PlanResourceChangeResponse) {
	h.t.Helper()
	r, err := h.srv.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: "ccp_k8s_cluster",
		PriorState: h.enc(prior), ProposedNewState: h.enc(h.proposed(prior, cfg)), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("plan", r.Diagnostics)
	return h.dec(r.PlannedState), r
}

func (h *ccksHarness) apply(prior, planned, cfg tftypes.Value, priv []byte) (tftypes.Value, []*tfprotov6.Diagnostic) {
	h.t.Helper()
	r, err := h.srv.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: "ccp_k8s_cluster",
		PriorState: h.enc(prior), PlannedState: h.enc(planned), Config: h.enc(cfg), PlannedPrivate: priv})
	if err != nil {
		h.t.Fatal(err)
	}
	if r.NewState == nil {
		return tftypes.NewValue(h.typ, nil), r.Diagnostics
	}
	return h.dec(r.NewState), r.Diagnostics
}

func hasError(diags []*tfprotov6.Diagnostic, sub string) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError && strings.Contains(d.Summary+" "+d.Detail, sub) {
			return true
		}
	}
	return false
}

func hasWarning(diags []*tfprotov6.Diagnostic, sub string) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityWarning && strings.Contains(d.Summary+" "+d.Detail, sub) {
			return true
		}
	}
	return false
}

func fastPolls(t *testing.T) {
	old := ccks.PollInterval
	ccks.PollInterval = time.Millisecond
	t.Cleanup(func() { ccks.PollInterval = old })
}

func ccksAttr(v tftypes.Value, name string) tftypes.Value {
	var m map[string]tftypes.Value
	_ = v.As(&m)
	return m[name]
}

func replaces(r *tfprotov6.PlanResourceChangeResponse, attr string) bool {
	for _, p := range r.RequiresReplace {
		if p.String() == tftypes.NewAttributePath().WithAttributeName(attr).String() {
			return true
		}
	}
	return false
}

func TestCCKSLegacyConfigurationPlansNothing(t *testing.T) {
	api := newFakeCCKSAPI()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

	// 1. Refresh of a pre-#2143 state, then the same pre-#2143 configuration.
	refreshed := h.read(h.legacyState())
	for name, want := range map[string]tftypes.Value{
		"plan": s("standard"), "tier": s("prod"), "k8s_version": s("v1.34.8"),
		"current_k8s_version": s("1.34.9"), "os_template_key": s("kube-v1-34-8"),
	} {
		if got := ccksAttr(refreshed, name); !got.Equal(want) {
			t.Errorf("refresh: %s = %v, want %v", name, got, want)
		}
	}
	if ccksAttr(refreshed, "maintenance").IsNull() {
		t.Error("refresh: maintenance should be read back (platform defaults)")
	}
	planned, r := h.plan(refreshed, h.config(nil))
	if !planned.Equal(refreshed) {
		d, _ := planned.Diff(refreshed)
		t.Errorf("tier = \"prod\": diff after the API migration: %v", d)
	}
	if len(r.RequiresReplace) != 0 {
		t.Errorf("tier = \"prod\": replacement planned: %v", r.RequiresReplace)
	}

	// 2. Migrating the configuration to `plan` changes nothing.
	nullStr := tftypes.NewValue(tftypes.String, nil)
	planned, r = h.plan(refreshed, h.config(map[string]tftypes.Value{"tier": nullStr, "plan": s("standard")}))
	if !planned.Equal(refreshed) || len(r.RequiresReplace) != 0 {
		d, _ := planned.Diff(refreshed)
		t.Errorf("plan = \"standard\": diff %v, replace %v", d, r.RequiresReplace)
	}

	// 3. standard → pro: in place, the etcd volume known after apply.
	planned, r = h.plan(refreshed, h.config(map[string]tftypes.Value{"tier": nullStr, "plan": s("pro")}))
	if len(r.RequiresReplace) != 0 {
		t.Errorf("standard → pro must be in place, replace %v", r.RequiresReplace)
	}
	if !ccksAttr(planned, "tier").Equal(s("prod")) || ccksAttr(planned, "etcd_volume_gb").IsKnown() {
		t.Errorf("standard → pro: tier %v, etcd_volume_gb %v", ccksAttr(planned, "tier"), ccksAttr(planned, "etcd_volume_gb"))
	}

	// 4. standard → starter (or tier dev): replacement on `plan`.
	for _, cfg := range []tftypes.Value{
		h.config(map[string]tftypes.Value{"tier": nullStr, "plan": s("starter")}),
		h.config(map[string]tftypes.Value{"tier": s("dev")}),
	} {
		planned, r = h.plan(refreshed, cfg)
		if !replaces(r, "plan") {
			t.Errorf("paid → starter must replace on plan, got %v", r.RequiresReplace)
		}
		if !ccksAttr(planned, "plan").Equal(s("starter")) || !ccksAttr(planned, "tier").Equal(s("dev")) {
			t.Errorf("paid → starter: plan %v tier %v", ccksAttr(planned, "plan"), ccksAttr(planned, "tier"))
		}
	}

	// 5. A tags change: in place (os_image unset is not "known after apply"
	// any more, which forced a replacement).
	tags := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{s("env:prod")})
	planned, r = h.plan(refreshed, h.config(map[string]tftypes.Value{"tags": tags}))
	if len(r.RequiresReplace) != 0 {
		t.Errorf("tags change must be in place, replace %v", r.RequiresReplace)
	}
	if !ccksAttr(planned, "os_image").Equal(s("flatcar")) {
		t.Errorf("os_image = %v, want the state value", ccksAttr(planned, "os_image"))
	}
	if !ccksAttr(planned, "maintenance").Equal(ccksAttr(refreshed, "maintenance")) {
		t.Errorf("maintenance unset must keep the state value on another change")
	}
	for _, c := range api.calls {
		if c[:3] != "GET" {
			t.Errorf("plan and refresh must only read, got %s", c)
		}
	}
}

// Blocking point 1: a state refreshed by v7.5.1 after a platform upgrade
// holds the REWRITTEN template key (kube-v1-34-9) and the normalised target
// (1.34.9). Upgrading the provider must not plan a replacement: the change
// back to the configured key is in place, applied without any call.
func TestCCKSv751RewrittenTemplateKeyIsNoReplacement(t *testing.T) {
	api := newFakeCCKSAPI()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	var lm map[string]tftypes.Value
	_ = h.legacyState().As(&lm)
	m := map[string]tftypes.Value{}
	for k, v := range lm {
		m[k] = v
	}
	m["os_template_key"], m["k8s_version"] = s("kube-v1-34-9"), s("1.34.9") // what v7.5.1 read back
	refreshed := h.read(tftypes.NewValue(h.typ, m))
	cfg := h.config(nil)
	planned, r := h.plan(refreshed, cfg)
	if len(r.RequiresReplace) != 0 {
		t.Fatalf("v7.5.1 state with a rewritten key: replacement planned %v", r.RequiresReplace)
	}
	newState, diags := h.apply(refreshed, planned, cfg, nil)
	h.fail("apply", diags)
	for _, c := range api.calls {
		if strings.HasPrefix(c, "POST") || strings.HasPrefix(c, "PUT") {
			t.Errorf("no write expected, got %s", c)
		}
	}
	if !ccksAttr(newState, "os_template_key").Equal(s("kube-v1-34-8")) {
		t.Errorf("os_template_key = %v", ccksAttr(newState, "os_template_key"))
	}
	again, r := h.plan(h.read(newState), cfg)
	if !again.Equal(h.read(newState)) || len(r.RequiresReplace) != 0 {
		t.Errorf("diff after the convergence apply")
	}
	// A real change of template (not two platform keys) still replaces.
	_, r = h.plan(refreshed, h.config(map[string]tftypes.Value{"os_template_key": s("clks-capi-debian-13")}))
	if !replaces(r, "os_template_key") {
		t.Errorf("a real template change must replace, got %v", r.RequiresReplace)
	}
}

// Blocking point 2: an upgrade fails; the platform keeps the TARGET in
// k8s_version. The failed apply is an error; the next plan still proposes the
// upgrade (the failure is not recorded as done); the 2nd apply relaunches it
// (#2146) and converges.
func TestCCKSFailedUpgradeThenSecondApply(t *testing.T) {
	fastPolls(t)
	api := newFakeCCKSAPI()
	api.cluster["k8s_version"], api.cluster["os_template_key"] = "v1.34.8", "kube-v1-34-8"
	api.cluster["k8s_version_effective"] = "1.34.8"
	api.failNext, api.relaunch = true, true
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	state := h.read(h.legacyState())
	cfg := h.config(map[string]tftypes.Value{"k8s_version": s("v1.35.0")})

	planned, _ := h.plan(state, cfg)
	_, diags := h.apply(state, planned, cfg, nil)
	if !hasError(diags, "rollout bloqué") {
		t.Fatalf("1st apply: the failure must be an error, got %v", diags)
	}
	// Nothing recorded (Terraform keeps the prior state on error).
	refreshed := h.read(state)
	if !ccksAttr(refreshed, "k8s_version").Equal(s("v1.34.8")) || !ccksAttr(refreshed, "status").Equal(s("error")) {
		t.Fatalf("after failure: k8s_version %v status %v", ccksAttr(refreshed, "k8s_version"), ccksAttr(refreshed, "status"))
	}
	if !ccksAttr(refreshed, "current_k8s_version").Equal(s("1.34.8")) {
		t.Errorf("current_k8s_version must be the effective version, got %v", ccksAttr(refreshed, "current_k8s_version"))
	}
	planned, _ = h.plan(refreshed, cfg)
	if !ccksAttr(planned, "k8s_version").Equal(s("v1.35.0")) {
		t.Fatalf("the failed upgrade must still be planned")
	}
	newState, diags := h.apply(refreshed, planned, cfg, nil)
	h.fail("2nd apply", diags)
	if !hasWarning(diags, "relaunched") {
		t.Errorf("2nd apply: the relaunch must be reported, got %v", diags)
	}
	if !ccksAttr(newState, "current_k8s_version").Equal(s("1.35.0")) {
		t.Errorf("current_k8s_version = %v", ccksAttr(newState, "current_k8s_version"))
	}
	again := h.read(newState)
	if p, r := h.plan(again, cfg); !p.Equal(again) || len(r.RequiresReplace) != 0 {
		d, _ := p.Diff(again)
		t.Errorf("diff after the relaunch: %v", d)
	}

	// Older API (no relaunch): the 2nd apply is a clear error, never a
	// success that records the failed target.
	api2 := newFakeCCKSAPI()
	api2.cluster["k8s_version"], api2.cluster["os_template_key"] = "v1.34.8", "kube-v1-34-8"
	api2.failNext = true
	ts2 := httptest.NewServer(api2)
	defer ts2.Close()
	h2 := newCCKSHarness(t, ts2.URL)
	st := h2.read(h2.legacyState())
	pl, _ := h2.plan(st, cfg)
	_, _ = h2.apply(st, pl, cfg, nil)
	st = h2.read(st)
	pl, _ = h2.plan(st, cfg)
	if _, diags := h2.apply(st, pl, cfg, nil); !hasError(diags, "refused to relaunch") {
		t.Fatalf("old API: want a clear error, got %v", diags)
	}
}

// Point 3: PUT /oidc refused at creation → a warning, the cluster is kept
// (not tainted), the next plan retries the SSO in place.
func TestCCKSCreateOIDCFailureIsRetriedInPlace(t *testing.T) {
	fastPolls(t)
	api := newFakeCCKSAPI()
	api.oidcStatus = http.StatusUnprocessableEntity
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	ot := h.typ.AttributeTypes["oidc"].(tftypes.Object)
	ov := map[string]tftypes.Value{}
	for k, at := range ot.AttributeTypes {
		ov[k] = tftypes.NewValue(at, nil)
	}
	ov["issuer_url"], ov["client_id"] = s("https://sso.example.com"), s("kubectl")
	cfg := h.config(map[string]tftypes.Value{"oidc": tftypes.NewValue(ot, ov)})
	null := tftypes.NewValue(h.typ, nil)
	planned, _ := h.plan(null, cfg)
	state, diags := h.apply(null, planned, cfg, nil)
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("create must not fail (it would taint the cluster): %s — %s", d.Summary, d.Detail)
		}
	}
	if !hasWarning(diags, "retried by the next apply") {
		t.Fatalf("want a warning, got %v", diags)
	}
	refreshed := h.read(state)
	if !ccksAttr(refreshed, "oidc").IsNull() {
		t.Fatalf("refresh must show the SSO absent")
	}
	if p, r := h.plan(refreshed, cfg); ccksAttr(p, "oidc").IsNull() || len(r.RequiresReplace) != 0 {
		t.Fatalf("next plan must retry in place (replace %v)", r.RequiresReplace)
	}
}

// Point 11: import rebuilds initial_pool from the oldest pool; a
// configuration that matches (disk_gb omitted) plans no replacement.
func TestCCKSImportRebuildsInitialPool(t *testing.T) {
	api := newFakeCCKSAPI()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	ir, err := h.srv.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{TypeName: "ccp_k8s_cluster", ID: "cl-1"})
	if err != nil {
		t.Fatal(err)
	}
	h.fail("import", ir.Diagnostics)
	rr, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: "ccp_k8s_cluster",
		CurrentState: ir.ImportedResources[0].State, Private: ir.ImportedResources[0].Private})
	if err != nil {
		t.Fatal(err)
	}
	h.fail("read", rr.Diagnostics)
	state := h.dec(rr.NewState)
	ip := ccksAttr(state, "initial_pool")
	if ip.IsNull() || !ccksAttr(ip, "name").Equal(tftypes.NewValue(tftypes.String, "default")) ||
		!ccksAttr(ip, "disk_gb").Equal(tftypes.NewValue(tftypes.Number, 20)) {
		t.Fatalf("initial_pool = %v", ip)
	}
	cfg := h.config(map[string]tftypes.Value{"os_template_key": tftypes.NewValue(tftypes.String, "kube-v1-34-9"),
		"k8s_version": tftypes.NewValue(tftypes.String, "1.34.9")})
	r, err := h.srv.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: "ccp_k8s_cluster",
		PriorState: rr.NewState, ProposedNewState: h.enc(h.proposed(state, cfg)), Config: h.enc(cfg), PriorPrivate: rr.Private})
	if err != nil {
		t.Fatal(err)
	}
	h.fail("plan", r.Diagnostics)
	if len(r.RequiresReplace) != 0 {
		t.Fatalf("imported cluster: replacement planned %v", r.RequiresReplace)
	}
}

// Point 9: a plan not known at plan time plans no replacement (never a
// destruction on a guess) with a warning; the apply refuses a crossing.
func TestCCKSUnknownPlan(t *testing.T) {
	api := newFakeCCKSAPI()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	state := h.read(h.legacyState())
	cfg := h.config(map[string]tftypes.Value{"tier": tftypes.NewValue(tftypes.String, nil),
		"plan": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
	_, r := h.plan(state, cfg)
	if len(r.RequiresReplace) != 0 || !hasWarning(r.Diagnostics, "not known") || !hasWarning(r.Diagnostics, "WILL FAIL") {
		t.Fatalf("replace %v, diags %v", r.RequiresReplace, r.Diagnostics)
	}
	// At apply the plan is known: starter from standard is refused.
	s := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	cfg = h.config(map[string]tftypes.Value{"tier": tftypes.NewValue(tftypes.String, nil), "plan": s("starter")})
	var sm map[string]tftypes.Value
	_ = state.As(&sm)
	m := map[string]tftypes.Value{} // a copy: As shares the value's map
	for k, v := range sm {
		m[k] = v
	}
	m["plan"], m["tier"] = s("starter"), s("dev")
	if _, diags := h.apply(state, tftypes.NewValue(h.typ, m), cfg, nil); !hasError(diags, "-replace") {
		t.Fatalf("crossing at apply must be refused, got %v", diags)
	}
}

// Point 10: values planned from the state (current_k8s_version,
// etcd_volume_gb, node_ip_block_size) stay as planned even when the platform
// moves them between plan and apply (automatic patch): no "inconsistent
// result after apply".
func TestCCKSPlannedFromStateStayConsistent(t *testing.T) {
	api := newFakeCCKSAPI()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	state := h.read(h.legacyState())
	tags := tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tftypes.NewValue(tftypes.String, "x")})
	cfg := h.config(map[string]tftypes.Value{"tags": tags})
	planned, _ := h.plan(state, cfg)
	api.mu.Lock()
	api.cluster["k8s_version"], api.cluster["k8s_version_effective"] = "1.34.10", "1.34.10"
	api.mu.Unlock()
	newState, diags := h.apply(state, planned, cfg, nil)
	h.fail("apply", diags)
	for _, a := range []string{"current_k8s_version", "etcd_volume_gb", "node_ip_block_size"} {
		if p := ccksAttr(planned, a); p.IsKnown() && !ccksAttr(newState, a).Equal(p) {
			t.Errorf("%s: planned %v, applied %v", a, p, ccksAttr(newState, a))
		}
	}
	if !ccksAttr(h.read(newState), "current_k8s_version").Equal(tftypes.NewValue(tftypes.String, "1.34.10")) {
		t.Error("the next refresh must read the new version")
	}
}

// Point 3 (maintenance): a PUT /maintenance refused at creation is a warning;
// the state matches the plan, the next refresh shows the real settings.
func TestCCKSCreateMaintenanceFailureIsAWarning(t *testing.T) {
	fastPolls(t)
	api := newFakeCCKSAPI() // no PUT /maintenance route: 404
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCCKSHarness(t, ts.URL)
	mt := h.typ.AttributeTypes["maintenance"].(tftypes.Object)
	mv := map[string]tftypes.Value{}
	for k, at := range mt.AttributeTypes {
		mv[k] = tftypes.NewValue(at, nil)
	}
	mv["auto_patch"] = tftypes.NewValue(tftypes.Bool, false)
	cfg := h.config(map[string]tftypes.Value{"maintenance": tftypes.NewValue(mt, mv)})
	null := tftypes.NewValue(h.typ, nil)
	planned, _ := h.plan(null, cfg)
	state, diags := h.apply(null, planned, cfg, nil)
	if hasError(diags, "") || !hasWarning(diags, "Maintenance settings not applied") {
		t.Fatalf("want a warning only, got %v", diags)
	}
	if !ccksAttr(ccksAttr(state, "maintenance"), "auto_patch").Equal(tftypes.NewValue(tftypes.Bool, false)) {
		t.Fatalf("state must match the plan")
	}
	if !ccksAttr(ccksAttr(h.read(state), "maintenance"), "auto_patch").Equal(tftypes.NewValue(tftypes.Bool, true)) {
		t.Fatalf("refresh must show the platform value (retry planned)")
	}
}

// Point 7: a version that is not X.Y.Z is refused at validation.
func TestCCKSVersionFormatValidated(t *testing.T) {
	h := newCCKSHarness(t, "http://127.0.0.1:1")
	cfg := h.config(map[string]tftypes.Value{"k8s_version": tftypes.NewValue(tftypes.String, "v1.35")})
	r, err := h.srv.ValidateResourceConfig(h.ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: "ccp_k8s_cluster", Config: h.enc(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(r.Diagnostics, "X.Y.Z") {
		t.Fatalf("v1.35 must be refused, got %v", r.Diagnostics)
	}
}
