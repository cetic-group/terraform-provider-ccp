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

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// cetic-cloud-platform#1734 — cycle complet de `ccp_vpc` à travers le vrai
// serveur du provider (plan, apply, refresh, plan) contre une API simulée qui
// suit le contrat : `dedicated_egress_ip` false par défaut, IP attribuée
// dans la requête qui active l'option, rendue dans celle qui la désactive.
//
// Ce que le test prouve : aucun diff après apply (ni perpétuel, ni sur un
// changement de statut fait par la plateforme), egress_* « known after
// apply » quand l'option change (jamais figés), modification en place sans
// remplacement, et PATCH qui ne porte que `dedicated_egress_ip`.

type fakeVPCAPI struct {
	mu      sync.Mutex
	vpc     map[string]any
	nextIP  []string
	posts   []map[string]any
	patches []string
}

func (f *fakeVPCAPI) setEgress(on bool) {
	f.vpc["dedicated_egress_ip"] = on
	if on {
		f.vpc["egress_ip"], f.nextIP = f.nextIP[0], f.nextIP[1:]
		f.vpc["egress_ip_type"], f.vpc["egress_ip_status"] = "edge", "allocated"
		return
	}
	f.vpc["egress_ip"], f.vpc["egress_ip_type"], f.vpc["egress_ip_status"] = nil, nil, nil
}

func (f *fakeVPCAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/vpcs":
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		f.posts = append(f.posts, req)
		f.vpc = map[string]any{
			"id": "vpc-1", "name": req["name"], "region": req["region"], "cidr": "10.1.0.0/16",
			"status": "active", "tags": []string{}, "created_at": "2026-11-03T10:00:00Z",
		}
		on, _ := req["dedicated_egress_ip"].(bool)
		f.setEgress(on)
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/vpcs/vpc-1" && f.vpc != nil:
	case r.Method == http.MethodPatch && r.URL.Path == "/v1/vpcs/vpc-1" && f.vpc != nil:
		f.patches = append(f.patches, string(body))
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if v, ok := req["dedicated_egress_ip"].(bool); ok && v != f.vpc["dedicated_egress_ip"] {
			f.setEgress(v)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"not found"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(f.vpc)
}

type vpcHarness struct {
	t   *testing.T
	ctx context.Context
	srv tfprotov6.ProviderServer
	typ tftypes.Object
}

func newVPCHarness(t *testing.T, url string) *vpcHarness {
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
	h := &vpcHarness{t: t, ctx: ctx, srv: srv, typ: sch.ResourceSchemas["ccp_vpc"].ValueType().(tftypes.Object)}
	h.fail("configure", cr.Diagnostics)
	return h
}

func (h *vpcHarness) fail(step string, diags []*tfprotov6.Diagnostic) {
	h.t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			h.t.Fatalf("%s: %s — %s", step, d.Summary, d.Detail)
		}
	}
}

func (h *vpcHarness) enc(v tftypes.Value) *tfprotov6.DynamicValue {
	h.t.Helper()
	d, err := tfprotov6.NewDynamicValue(h.typ, v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func (h *vpcHarness) dec(d *tfprotov6.DynamicValue) tftypes.Value {
	h.t.Helper()
	v, err := d.Unmarshal(h.typ)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

// config builds a configuration: name + region, plus the given attributes.
func (h *vpcHarness) config(set map[string]tftypes.Value) tftypes.Value {
	m := map[string]tftypes.Value{}
	for k, at := range h.typ.AttributeTypes {
		m[k] = tftypes.NewValue(at, nil)
	}
	m["name"] = tftypes.NewValue(tftypes.String, "prod")
	m["region"] = tftypes.NewValue(tftypes.String, "RNN")
	for k, v := range set {
		m[k] = v
	}
	return tftypes.NewValue(h.typ, m)
}

// proposed mimics Terraform core: config values, prior state where the
// configuration leaves a (computed) attribute null.
func (h *vpcHarness) proposed(prior, cfg tftypes.Value) tftypes.Value {
	var c, p map[string]tftypes.Value
	_ = cfg.As(&c)
	if prior.IsNull() {
		return cfg
	}
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

func (h *vpcHarness) plan(prior, cfg tftypes.Value) (tftypes.Value, *tfprotov6.PlanResourceChangeResponse) {
	h.t.Helper()
	r, err := h.srv.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: "ccp_vpc",
		PriorState: h.enc(prior), ProposedNewState: h.enc(h.proposed(prior, cfg)), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("plan", r.Diagnostics)
	return h.dec(r.PlannedState), r
}

func (h *vpcHarness) apply(prior, planned, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: "ccp_vpc",
		PriorState: h.enc(prior), PlannedState: h.enc(planned), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("apply", r.Diagnostics)
	return h.dec(r.NewState)
}

func (h *vpcHarness) read(state tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: "ccp_vpc", CurrentState: h.enc(state)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("read", r.Diagnostics)
	return h.dec(r.NewState)
}

func vpcAttr(v tftypes.Value, name string) tftypes.Value {
	var m map[string]tftypes.Value
	_ = v.As(&m)
	return m[name]
}

// assertNoDiff: refresh then plan with the same configuration must plan the
// prior state unchanged, without replacement.
func (h *vpcHarness) assertNoDiff(step string, state, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	refreshed := h.read(state)
	planned, r := h.plan(refreshed, cfg)
	if !planned.Equal(refreshed) {
		diff, _ := planned.Diff(refreshed)
		h.t.Errorf("%s: diff après apply: %v", step, diff)
	}
	if len(r.RequiresReplace) != 0 {
		h.t.Errorf("%s: remplacement planifié: %v", step, r.RequiresReplace)
	}
	return refreshed
}

func TestVPCDedicatedEgressLifecycle(t *testing.T) {
	api := &fakeVPCAPI{nextIP: []string{"203.0.113.7", "203.0.113.8", "203.0.113.9"}}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newVPCHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	on := map[string]tftypes.Value{"dedicated_egress_ip": tftypes.NewValue(tftypes.Bool, true)}
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	nullStr := tftypes.NewValue(tftypes.String, nil)

	// 1. Création sans l'option : défaut false, envoyé explicitement.
	cfg := h.config(nil)
	planned, _ := h.plan(null, cfg)
	if !vpcAttr(planned, "dedicated_egress_ip").Equal(tftypes.NewValue(tftypes.Bool, false)) {
		t.Fatalf("défaut : dedicated_egress_ip=false attendu dans le plan, reçu %v", vpcAttr(planned, "dedicated_egress_ip"))
	}
	state := h.apply(null, planned, cfg)
	if got := api.posts[0]["dedicated_egress_ip"]; got != false {
		t.Errorf("POST : dedicated_egress_ip=false attendu, reçu %v", got)
	}
	if !vpcAttr(state, "egress_ip").Equal(nullStr) {
		t.Errorf("sans l'option : egress_ip null attendu, reçu %v", vpcAttr(state, "egress_ip"))
	}
	state = h.assertNoDiff("création sans l'option", state, cfg)

	// 2. Activation : en place, egress_* inconnus au plan, PATCH du seul champ.
	cfg = h.config(on)
	planned, r := h.plan(state, cfg)
	if len(r.RequiresReplace) != 0 {
		t.Fatalf("activation : remplacement planifié %v (doit être en place)", r.RequiresReplace)
	}
	for _, a := range []string{"egress_ip", "egress_ip_type", "egress_ip_status"} {
		if vpcAttr(planned, a).IsKnown() {
			t.Errorf("activation : %s doit être « known after apply », planifié %v", a, vpcAttr(planned, a))
		}
	}
	for _, a := range []string{"id", "status", "created_at", "cidr"} {
		if !vpcAttr(planned, a).Equal(vpcAttr(state, a)) {
			t.Errorf("activation : %s ne doit pas changer, planifié %v", a, vpcAttr(planned, a))
		}
	}
	state = h.apply(state, planned, cfg)
	if len(api.patches) != 1 || api.patches[0] != `{"dedicated_egress_ip":true}` {
		t.Fatalf("PATCH : un seul appel avec {\"dedicated_egress_ip\":true} attendu, reçu %q", api.patches)
	}
	if !vpcAttr(state, "egress_ip").Equal(str("203.0.113.7")) {
		t.Errorf("activation : egress_ip=203.0.113.7 attendu dans l'état, reçu %v", vpcAttr(state, "egress_ip"))
	}
	state = h.assertNoDiff("activation", state, cfg)

	// 3. La plateforme met l'IP en service : le refresh suit, aucun diff.
	api.mu.Lock()
	api.vpc["egress_ip_status"] = "attached"
	api.mu.Unlock()
	state = h.assertNoDiff("IP mise en service", state, cfg)
	if !vpcAttr(state, "egress_ip_status").Equal(str("attached")) {
		t.Errorf("refresh : egress_ip_status=attached attendu, reçu %v", vpcAttr(state, "egress_ip_status"))
	}
	if len(api.patches) != 1 {
		t.Errorf("aucun PATCH attendu sans changement, reçu %q", api.patches)
	}

	// 4. Attribut retiré de la configuration : retour au défaut false, IP rendue.
	cfg = h.config(nil)
	planned, r = h.plan(state, cfg)
	if len(r.RequiresReplace) != 0 {
		t.Fatalf("désactivation : remplacement planifié %v", r.RequiresReplace)
	}
	if !vpcAttr(planned, "dedicated_egress_ip").Equal(tftypes.NewValue(tftypes.Bool, false)) {
		t.Fatalf("attribut retiré : false planifié attendu, reçu %v", vpcAttr(planned, "dedicated_egress_ip"))
	}
	if vpcAttr(planned, "egress_ip").IsKnown() {
		t.Errorf("désactivation : egress_ip ne doit pas rester figé à %v", vpcAttr(planned, "egress_ip"))
	}
	state = h.apply(state, planned, cfg)
	if len(api.patches) != 2 || api.patches[1] != `{"dedicated_egress_ip":false}` {
		t.Fatalf("PATCH : {\"dedicated_egress_ip\":false} attendu, reçu %q", api.patches)
	}
	if !vpcAttr(state, "egress_ip").Equal(nullStr) || !vpcAttr(state, "egress_ip_status").Equal(nullStr) {
		t.Errorf("désactivation : egress_* null attendus, reçu %v %v", vpcAttr(state, "egress_ip"), vpcAttr(state, "egress_ip_status"))
	}
	h.assertNoDiff("désactivation", state, cfg)
}

func TestVPCCreatedWithDedicatedEgress(t *testing.T) {
	api := &fakeVPCAPI{nextIP: []string{"203.0.113.7"}}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newVPCHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	cfg := h.config(map[string]tftypes.Value{"dedicated_egress_ip": tftypes.NewValue(tftypes.Bool, true)})

	planned, _ := h.plan(null, cfg)
	state := h.apply(null, planned, cfg)
	if got := api.posts[0]["dedicated_egress_ip"]; got != true {
		t.Errorf("POST : dedicated_egress_ip=true attendu, reçu %v", got)
	}
	if !vpcAttr(state, "egress_ip").Equal(tftypes.NewValue(tftypes.String, "203.0.113.7")) {
		t.Errorf("egress_ip attendu dès l'apply de création, reçu %v", vpcAttr(state, "egress_ip"))
	}
	h.assertNoDiff("création avec l'option", state, cfg)
	if len(api.patches) != 0 {
		t.Errorf("aucun PATCH attendu, reçu %q", api.patches)
	}
}

// Une API antérieure à #1734 ignore le champ : erreur explicite, pas de
// « Provider produced inconsistent result ».
func TestVPCCreateOnPlatformWithoutTheOption(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = w.Write([]byte(`{"id":"vpc-1","name":"prod","region":"RNN","cidr":"10.1.0.0/16",` +
			`"status":"active","tags":[],"created_at":"2026-11-03T10:00:00Z"}`))
	}))
	defer ts.Close()
	h := newVPCHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	cfg := h.config(map[string]tftypes.Value{"dedicated_egress_ip": tftypes.NewValue(tftypes.Bool, true)})
	planned, _ := h.plan(null, cfg)
	r, err := h.srv.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: "ccp_vpc",
		PriorState: h.enc(null), PlannedState: h.enc(planned), Config: h.enc(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range r.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError && strings.Contains(d.Summary, "not supported by the platform") {
			found = true
		}
	}
	if !found {
		t.Fatalf("erreur « not supported by the platform » attendue, reçu %+v", r.Diagnostics)
	}
	if h.dec(r.NewState).IsNull() {
		t.Error("le VPC créé doit rester dans l'état (marqué tainted)")
	}
}

// Refus de l'API (409 pool vide) à l'activation : message lisible, état
// inchangé.
func TestVPCEnableRefusedWhenPoolIsEmpty(t *testing.T) {
	api := &fakeVPCAPI{nextIP: []string{"unused"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/vpcs/vpc-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"detail":"Aucune IP publique n'est disponible pour la sortie internet du VPC dans la région RNN"}`))
			return
		}
		api.ServeHTTP(w, r)
	})
	mux.Handle("/", api)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	h := newVPCHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	cfg := h.config(nil)
	planned, _ := h.plan(null, cfg)
	state := h.apply(null, planned, cfg)

	cfg = h.config(map[string]tftypes.Value{"dedicated_egress_ip": tftypes.NewValue(tftypes.Bool, true)})
	planned, _ = h.plan(state, cfg)
	r, err := h.srv.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: "ccp_vpc",
		PriorState: h.enc(state), PlannedState: h.enc(planned), Config: h.enc(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	var detail string
	for _, d := range r.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			detail = d.Detail
		}
	}
	if !strings.Contains(detail, "Aucune IP publique") || !strings.Contains(detail, "no public IP is free") {
		t.Errorf("409 : détail de l'API et indication attendus, reçu %q", detail)
	}
}
