package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1":
		_ = json.NewEncoder(w).Encode(f.cluster)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/k8s/clusters/cl-1/maintenance":
		_ = json.NewEncoder(w).Encode(f.maint)
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
