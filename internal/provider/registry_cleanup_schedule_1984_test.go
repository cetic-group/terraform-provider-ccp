package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// cetic-cloud-platform#1984 — cycle complet de `ccp_registry_cleanup_schedule`
// à travers le vrai serveur du provider (validate, plan, apply, refresh, plan,
// import) contre une API simulée qui suit le code de la plateforme :
//
//   - POST/PATCH normalisent comme l'API (nom et motifs `strip()`, cron aux
//     espaces réduits, cron blanc → null) ;
//   - PATCH : champ absent = inchangé, `"cron": null` = manuelle, changer
//     `critere` sans `valeur` → 422 ;
//   - au plus 4 planifications (422), nom unique (409) ;
//   - pas de GET unitaire : lecture par la liste.
//
// Ce que le test prouve : jamais d'état différent du plan après apply (sinon
// Terraform refuse : « Provider produced inconsistent result »), aucun diff
// après refresh, retrait de `cron` = PATCH `"cron": null`, corps complet à
// chaque écriture, dérive détectée, import convergent.

type fakeCleanupAPI struct {
	mu      sync.Mutex
	scheds  []map[string]any
	n       int
	posts   []map[string]any
	patches []map[string]any
	deletes int
	// registryGone: la registry a été supprimée — la liste répond 404.
	registryGone bool
}

func pyStripT(s string) string { return strings.TrimSpace(s) }

func normPatterns(v any) []any {
	out := []any{}
	for _, p := range v.([]any) {
		out = append(out, pyStripT(p.(string)))
	}
	return out
}

func normCron(v any) any {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil
	}
	return strings.Join(f, " ")
}

func (f *fakeCleanupAPI) reply(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func (f *fakeCleanupAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	const base = "/v1/registries/reg-1/cleanup/schedules"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == base && f.registryGone:
		f.reply(w, http.StatusNotFound, map[string]any{"detail": "Registry introuvable."})
	case r.Method == http.MethodGet && r.URL.Path == base:
		f.reply(w, http.StatusOK, f.scheds)
	case r.Method == http.MethodPost && r.URL.Path == base:
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		f.posts = append(f.posts, req)
		if len(f.scheds) >= 4 {
			f.reply(w, http.StatusUnprocessableEntity, map[string]any{"detail": "Au plus 4 planifications de nettoyage par registry : supprimez-en une avant d'en créer une autre."})
			return
		}
		for _, s := range f.scheds {
			if s["name"] == pyStripT(req["name"].(string)) {
				f.reply(w, http.StatusConflict, map[string]any{"detail": "Une planification porte déjà ce nom."})
				return
			}
		}
		f.n++
		s := map[string]any{
			"id": fmt.Sprintf("cs-%d", f.n), "slot": len(f.scheds) + 1,
			"name": pyStripT(req["name"].(string)), "repo_patterns": normPatterns(req["repo_patterns"]),
			"tag_patterns": normPatterns(req["tag_patterns"]), "critere": req["critere"], "valeur": req["valeur"],
			"cron": normCron(req["cron"]), "enabled": req["enabled"],
			"last_dispatched_at": "2026-11-15T10:00:00Z", "created_at": "2026-11-15T10:00:00Z", "updated_at": "2026-11-15T10:00:00Z",
		}
		f.scheds = append(f.scheds, s)
		f.reply(w, http.StatusCreated, s)
	case strings.HasPrefix(r.URL.Path, base+"/"):
		id := strings.TrimPrefix(r.URL.Path, base+"/")
		idx := -1
		for i, s := range f.scheds {
			if s["id"] == id {
				idx = i
			}
		}
		if idx < 0 {
			f.reply(w, http.StatusNotFound, map[string]any{"detail": "Planification de nettoyage introuvable."})
			return
		}
		if r.Method == http.MethodDelete {
			f.deletes++
			f.scheds = append(f.scheds[:idx], f.scheds[idx+1:]...)
			f.reply(w, http.StatusNoContent, nil)
			return
		}
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		f.patches = append(f.patches, req)
		s := f.scheds[idx]
		if c, ok := req["critere"]; ok && c != s["critere"] {
			if _, has := req["valeur"]; !has && c != "all" {
				f.reply(w, http.StatusUnprocessableEntity, map[string]any{"detail": "« valeur » est exigée quand « critere » change."})
				return
			}
		}
		for k, v := range req {
			switch k {
			case "name":
				s[k] = pyStripT(v.(string))
			case "repo_patterns", "tag_patterns":
				s[k] = normPatterns(v)
			case "cron":
				s[k] = normCron(v)
			default:
				s[k] = v
			}
		}
		if req["critere"] == "all" {
			s["valeur"] = nil
		}
		s["last_dispatched_at"], s["updated_at"] = "2026-11-15T11:00:00Z", "2026-11-15T11:00:00Z"
		f.reply(w, http.StatusOK, s)
	default:
		f.reply(w, http.StatusNotFound, map[string]any{"detail": "not found"})
	}
}

const cleanupType = "ccp_registry_cleanup_schedule"

type cleanupHarness struct {
	*vpcHarness
}

func newCleanupHarness(t *testing.T, url string) *cleanupHarness {
	t.Helper()
	h := newVPCHarness(t, url)
	sch, err := h.srv.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	rs, ok := sch.ResourceSchemas[cleanupType]
	if !ok {
		t.Fatalf("%s absent du provider", cleanupType)
	}
	h.typ = rs.ValueType().(tftypes.Object)
	return &cleanupHarness{h}
}

func (h *cleanupHarness) cfg(set map[string]any) tftypes.Value {
	m := map[string]tftypes.Value{}
	for k, at := range h.typ.AttributeTypes {
		m[k] = tftypes.NewValue(at, nil)
	}
	base := map[string]any{
		"registry_id": "reg-1", "name": " nightly", "repo_patterns": []string{" app/*"},
		"tag_patterns": []string{"*-rc-*", "*-dev "}, "criterion": "keep_last_n", "value": 5,
		"cron": "0  3 * * *",
	}
	for k, v := range set {
		base[k] = v
	}
	for k, v := range base {
		at := h.typ.AttributeTypes[k]
		switch x := v.(type) {
		case nil:
			m[k] = tftypes.NewValue(at, nil)
		case []string:
			el := []tftypes.Value{}
			for _, s := range x {
				el = append(el, tftypes.NewValue(tftypes.String, s))
			}
			m[k] = tftypes.NewValue(at, el)
		case int:
			m[k] = tftypes.NewValue(at, x)
		default:
			m[k] = tftypes.NewValue(at, x)
		}
	}
	return tftypes.NewValue(h.typ, m)
}

// computedAttrs: seuls les attributs Computed (ou Optional+Computed) reprennent
// l'état antérieur quand la configuration les laisse null — comme le
// ProposedNewState de Terraform core. Un Optional pur (`cron`, `value`) retiré
// de la configuration est proposé null.
var computedAttrs = map[string]bool{"id": true, "slot": true, "last_dispatched_at": true,
	"created_at": true, "updated_at": true, "enabled": true}

func (h *cleanupHarness) proposed(prior, cfg tftypes.Value) tftypes.Value {
	if prior.IsNull() {
		return cfg
	}
	var c, p map[string]tftypes.Value
	_ = cfg.As(&c)
	_ = prior.As(&p)
	out := map[string]tftypes.Value{}
	for k, v := range c {
		if v.IsNull() && computedAttrs[k] {
			out[k] = p[k]
		} else {
			out[k] = v
		}
	}
	return tftypes.NewValue(h.typ, out)
}

func (h *cleanupHarness) validate(cfg tftypes.Value) []*tfprotov6.Diagnostic {
	h.t.Helper()
	r, err := h.srv.ValidateResourceConfig(h.ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: cleanupType, Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	return r.Diagnostics
}

func (h *cleanupHarness) plan(prior, cfg tftypes.Value) (tftypes.Value, *tfprotov6.PlanResourceChangeResponse) {
	h.t.Helper()
	h.fail("validate", h.validate(cfg))
	r, err := h.srv.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: cleanupType,
		PriorState: h.enc(prior), ProposedNewState: h.enc(h.proposed(prior, cfg)), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("plan", r.Diagnostics)
	return h.dec(r.PlannedState), r
}

func (h *cleanupHarness) applyResp(prior, planned, cfg tftypes.Value) *tfprotov6.ApplyResourceChangeResponse {
	h.t.Helper()
	r, err := h.srv.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: cleanupType,
		PriorState: h.enc(prior), PlannedState: h.enc(planned), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

// apply also checks what Terraform core checks: every value known in the plan
// comes back unchanged.
func (h *cleanupHarness) apply(prior, planned, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	r := h.applyResp(prior, planned, cfg)
	h.fail("apply", r.Diagnostics)
	state := h.dec(r.NewState)
	var p, s map[string]tftypes.Value
	_ = planned.As(&p)
	_ = state.As(&s)
	for k, v := range p {
		if v.IsFullyKnown() && !v.Equal(s[k]) {
			h.t.Errorf("inconsistent result after apply : %s planifié %v, rendu %v", k, v, s[k])
		}
		if !s[k].IsFullyKnown() {
			h.t.Errorf("%s inconnu après apply", k)
		}
	}
	return state
}

func (h *cleanupHarness) read(state tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: cleanupType, CurrentState: h.enc(state)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("read", r.Diagnostics)
	return h.dec(r.NewState)
}

func (h *cleanupHarness) assertNoDiff(step string, state, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	refreshed := h.read(state)
	planned, r := h.plan(refreshed, cfg)
	if !planned.Equal(refreshed) {
		diff, _ := planned.Diff(refreshed)
		h.t.Errorf("%s : diff après refresh : %v", step, diff)
	}
	if len(r.RequiresReplace) != 0 {
		h.t.Errorf("%s : remplacement planifié %v", step, r.RequiresReplace)
	}
	return refreshed
}

func attrOf(v tftypes.Value, name string) tftypes.Value { return vpcAttr(v, name) }

func TestRegistryCleanupScheduleLifecycle(t *testing.T) {
	api := &fakeCleanupAPI{}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCleanupHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

	// 1. Création : nom, motifs et cron non normalisés dans la configuration.
	cfg := h.cfg(nil)
	planned, _ := h.plan(null, cfg)
	if !attrOf(planned, "enabled").Equal(tftypes.NewValue(tftypes.Bool, true)) {
		t.Errorf("enabled : défaut true attendu au plan, reçu %v", attrOf(planned, "enabled"))
	}
	state := h.apply(null, planned, cfg)
	post := api.posts[0]
	for _, k := range []string{"name", "repo_patterns", "tag_patterns", "critere", "valeur", "cron", "enabled"} {
		if _, ok := post[k]; !ok {
			t.Errorf("POST : champ %s absent du corps %v", k, post)
		}
	}
	if len(post) != 7 {
		t.Errorf("POST : 7 champs attendus (extra=forbid côté API), reçu %v", post)
	}
	if !attrOf(state, "name").Equal(str(" nightly")) || !attrOf(state, "cron").Equal(str("0  3 * * *")) {
		t.Errorf("l'état doit garder l'écriture de l'utilisateur : %v %v", attrOf(state, "name"), attrOf(state, "cron"))
	}
	if !attrOf(state, "slot").Equal(tftypes.NewValue(tftypes.Number, 1)) || !attrOf(state, "id").Equal(str("cs-1")) {
		t.Errorf("slot/id : %v %v", attrOf(state, "slot"), attrOf(state, "id"))
	}
	state = h.assertNoDiff("création", state, cfg)

	// 2. Retrait du cron : planification manuelle, PATCH `"cron": null` explicite.
	cfg = h.cfg(map[string]any{"cron": nil})
	planned, r := h.plan(state, cfg)
	if len(r.RequiresReplace) != 0 {
		t.Fatalf("retrait du cron : remplacement planifié %v", r.RequiresReplace)
	}
	if attrOf(planned, "updated_at").IsKnown() || attrOf(planned, "last_dispatched_at").IsKnown() {
		t.Errorf("updated_at / last_dispatched_at doivent être inconnus au plan d'une modification")
	}
	if !attrOf(planned, "slot").Equal(attrOf(state, "slot")) || !attrOf(planned, "created_at").Equal(attrOf(state, "created_at")) {
		t.Errorf("slot / created_at ne changent pas")
	}
	state = h.apply(state, planned, cfg)
	patch := api.patches[0]
	if v, ok := patch["cron"]; !ok || v != nil {
		t.Fatalf("PATCH : \"cron\": null explicite attendu, reçu %v", patch)
	}
	if len(patch) != 7 {
		t.Errorf("PATCH : corps complet attendu, reçu %v", patch)
	}
	if api.scheds[0]["cron"] != nil {
		t.Errorf("API : cron non effacé")
	}
	state = h.assertNoDiff("retrait du cron", state, cfg)

	// 3. Changement de critère vers `all` sans value : valeur null envoyée.
	cfg = h.cfg(map[string]any{"cron": nil, "criterion": "all", "value": nil})
	planned, _ = h.plan(state, cfg)
	state = h.apply(state, planned, cfg)
	if v, ok := api.patches[1]["valeur"]; !ok || v != nil {
		t.Errorf("PATCH : \"valeur\": null explicite attendu, reçu %v", api.patches[1])
	}
	state = h.assertNoDiff("critère all", state, cfg)

	// 4. Changement vers older_than_days : la valeur part avec le critère (pas de 422).
	cfg = h.cfg(map[string]any{"cron": "*/30 * * * *", "criterion": "older_than_days", "value": 30})
	planned, _ = h.plan(state, cfg)
	state = h.apply(state, planned, cfg)
	state = h.assertNoDiff("older_than_days", state, cfg)

	// 5. Dérive : désactivée depuis la console → le refresh la voit, le plan la corrige.
	api.mu.Lock()
	api.scheds[0]["enabled"] = false
	api.mu.Unlock()
	refreshed := h.read(state)
	if !attrOf(refreshed, "enabled").Equal(tftypes.NewValue(tftypes.Bool, false)) {
		t.Fatalf("dérive non vue : enabled=%v", attrOf(refreshed, "enabled"))
	}
	planned, _ = h.plan(refreshed, cfg)
	if !attrOf(planned, "enabled").Equal(tftypes.NewValue(tftypes.Bool, true)) {
		t.Errorf("dérive : le plan doit ramener enabled=true")
	}
	state = h.apply(refreshed, planned, cfg)
	state = h.assertNoDiff("dérive corrigée", state, cfg)

	// 6. Import `<registry_id>/<schedule_id>` puis plan avec la configuration
	// écrite sous la forme normalisée : aucun diff.
	ir, err := h.srv.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{TypeName: cleanupType, ID: "reg-1/cs-1"})
	if err != nil {
		t.Fatal(err)
	}
	h.fail("import", ir.Diagnostics)
	imported := h.read(h.dec(ir.ImportedResources[0].State))
	icfg := h.cfg(map[string]any{"name": "nightly", "repo_patterns": []string{"app/*"}, "tag_patterns": []string{"*-rc-*", "*-dev"},
		"cron": "*/30 * * * *", "criterion": "older_than_days", "value": 30})
	h.assertNoDiff("import", imported, icfg)
	if bad, _ := h.srv.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{TypeName: cleanupType, ID: "cs-1"}); len(bad.Diagnostics) == 0 {
		t.Errorf("import sans registry_id : erreur attendue")
	}

	// 7. Suppression hors Terraform : le refresh retire la ressource.
	api.mu.Lock()
	saved := api.scheds
	api.scheds = nil
	api.mu.Unlock()
	if got := h.read(state); !got.IsNull() {
		t.Errorf("planification disparue : état null attendu, reçu %v", got)
	}
	api.mu.Lock()
	api.scheds = saved
	api.mu.Unlock()

	// 8. Destroy.
	dr := h.applyResp(state, null, null)
	h.fail("destroy", dr.Diagnostics)
	if api.deletes != 1 || len(api.scheds) != 0 {
		t.Errorf("destroy : 1 DELETE attendu, reçu %d (reste %d)", api.deletes, len(api.scheds))
	}
}

// Le refus de l'API (5e planification, nom pris) remonte tel quel.
func TestRegistryCleanupScheduleAPIRefusalsAreRelayed(t *testing.T) {
	api := &fakeCleanupAPI{}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCleanupHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	for i := 1; i <= 4; i++ {
		cfg := h.cfg(map[string]any{"name": fmt.Sprintf("s%d", i)})
		planned, _ := h.plan(null, cfg)
		h.apply(null, planned, cfg)
	}
	for _, tc := range []struct{ name, want string }{
		{"s5", "Au plus 4 planifications"},
	} {
		cfg := h.cfg(map[string]any{"name": tc.name})
		planned, _ := h.plan(null, cfg)
		r := h.applyResp(null, planned, cfg)
		if len(r.Diagnostics) == 0 || !strings.Contains(r.Diagnostics[0].Detail, tc.want) {
			t.Errorf("%s : message de l'API attendu (%q), reçu %v", tc.name, tc.want, r.Diagnostics)
		}
	}
	api.scheds = api.scheds[:3]
	cfg := h.cfg(map[string]any{"name": " s1 "})
	planned, _ := h.plan(null, cfg)
	r := h.applyResp(null, planned, cfg)
	if len(r.Diagnostics) == 0 || !strings.Contains(r.Diagnostics[0].Detail, "porte déjà ce nom") {
		t.Errorf("nom pris : 409 de l'API attendu, reçu %v", r.Diagnostics)
	}
}

// `terraform validate` côté plan : refus alignés sur l'API, valeurs inconnues
// jamais jugées (module consommateur).
func TestRegistryCleanupScheduleValidateConfig(t *testing.T) {
	h := newCleanupHarness(t, "http://127.0.0.1:1")
	hasErr := func(d []*tfprotov6.Diagnostic) bool {
		for _, x := range d {
			if x.Severity == tfprotov6.DiagnosticSeverityError {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		set  map[string]any
		fail bool
	}{
		{map[string]any{}, false},
		{map[string]any{"cron": ""}, false}, // blanc = manuelle, comme l'API
		{map[string]any{"criterion": "all", "value": nil}, false},
		{map[string]any{"criterion": "all", "value": 3}, true},
		{map[string]any{"criterion": "all", "value": nil, "repo_patterns": []string{"*/*"}, "tag_patterns": []string{"?*"}}, true},
		{map[string]any{"criterion": "older_than_days", "value": 6, "repo_patterns": []string{"*"}, "tag_patterns": []string{"*"}}, true},
		{map[string]any{"criterion": "older_than_days", "value": 7, "repo_patterns": []string{"*"}, "tag_patterns": []string{"*"}}, false},
		{map[string]any{"criterion": "keep_last_n", "value": nil}, true},
		{map[string]any{"criterion": "never"}, true},
		{map[string]any{"cron": "0 3 1 * 1"}, true},
		{map[string]any{"cron": "*,5 * * * *"}, true},
		{map[string]any{"repo_patterns": []string{"App/*"}}, true},
		{map[string]any{"tag_patterns": []string{"a", " a"}}, true},
		{map[string]any{"name": "-x"}, true},
	} {
		if got := hasErr(h.validate(h.cfg(tc.set))); got != tc.fail {
			t.Errorf("%v : erreur=%v, attendu %v", tc.set, got, tc.fail)
		}
	}
	// Valeurs inconnues (variables d'un module) : rien n'est jugé.
	m := map[string]tftypes.Value{}
	for k, at := range h.typ.AttributeTypes {
		m[k] = tftypes.NewValue(at, tftypes.UnknownValue)
	}
	for _, k := range []string{"id", "slot", "last_dispatched_at", "created_at", "updated_at"} {
		m[k] = tftypes.NewValue(h.typ.AttributeTypes[k], nil)
	}
	if d := h.validate(tftypes.NewValue(h.typ, m)); hasErr(d) {
		t.Errorf("configuration inconnue jugée : %v", d)
	}
}

// Une réécriture que l'API normaliserait à l'identique (espaces autour du nom
// ou d'un motif, cron reformaté, cron absent ↔ "") n'envoie AUCUN PATCH : un
// PATCH, même vide, remet `last_dispatched_at` à maintenant et peut faire
// sauter un passage dû. L'état prend le plan, sans diff ensuite.
func TestRegistryCleanupScheduleNoOpRespellingSendsNoPatch(t *testing.T) {
	api := &fakeCleanupAPI{}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCleanupHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	normal := map[string]any{"name": "nightly", "repo_patterns": []string{"app/*"}, "tag_patterns": []string{"*-rc-*", "*-dev"},
		"cron": "0 3 * * *"}
	with := func(over map[string]any) tftypes.Value {
		m := map[string]any{}
		for k, v := range normal {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		return h.cfg(m)
	}
	cfg := with(nil)
	planned, _ := h.plan(null, cfg)
	state := h.apply(null, planned, cfg)
	dispatched := attrOf(state, "last_dispatched_at")

	for _, tc := range []struct {
		step string
		over map[string]any
	}{
		{"nom avec espace", map[string]any{"name": " nightly"}},
		{"motifs avec espaces", map[string]any{"name": " nightly", "repo_patterns": []string{" app/* "}}},
		{"cron reformaté", map[string]any{"cron": "0  3 * * *"}},
		{"cron absent", map[string]any{"cron": nil}},
	} {
		if tc.step == "cron absent" {
			// Planification manuelle réelle (1 PATCH), puis "" ↔ absent sans PATCH.
			cfg = with(map[string]any{"cron": nil})
			planned, _ = h.plan(state, cfg)
			state = h.apply(state, planned, cfg)
			state = h.read(state)
			dispatched = attrOf(state, "last_dispatched_at")
			before := len(api.patches)
			for _, c := range []any{"", nil, "   "} {
				cfg = with(map[string]any{"cron": c})
				planned, _ = h.plan(state, cfg)
				state = h.apply(state, planned, cfg)
				if len(api.patches) != before {
					t.Errorf("cron %q : aucun PATCH attendu, reçu %v", c, api.patches[before:])
				}
				state = h.assertNoDiff(fmt.Sprintf("cron %q", c), state, cfg)
			}
			if !attrOf(state, "last_dispatched_at").Equal(dispatched) {
				t.Errorf("last_dispatched_at a bougé sans PATCH")
			}
			continue
		}
		before := len(api.patches)
		cfg = with(tc.over)
		planned, _ = h.plan(state, cfg)
		state = h.apply(state, planned, cfg)
		if len(api.patches) != before {
			t.Errorf("%s : aucun PATCH attendu, reçu %v", tc.step, api.patches[before:])
		}
		if !attrOf(state, "last_dispatched_at").Equal(dispatched) {
			t.Errorf("%s : last_dispatched_at doit rester %v, reçu %v", tc.step, dispatched, attrOf(state, "last_dispatched_at"))
		}
		state = h.assertNoDiff(tc.step, state, cfg)
	}
	// Contrôle : un vrai changement part bien.
	before := len(api.patches)
	cfg = with(map[string]any{"cron": nil, "enabled": false})
	planned, _ = h.plan(state, cfg)
	h.apply(state, planned, cfg)
	if len(api.patches) != before+1 {
		t.Errorf("changement réel : 1 PATCH attendu, reçu %d", len(api.patches)-before)
	}
}

// Registry supprimée : la liste répond 404, la planification sort de l'état
// sans erreur (pas d'échec du refresh).
func TestRegistryCleanupScheduleRegistryGone(t *testing.T) {
	api := &fakeCleanupAPI{}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCleanupHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	cfg := h.cfg(nil)
	planned, _ := h.plan(null, cfg)
	state := h.apply(null, planned, cfg)
	api.mu.Lock()
	api.registryGone = true
	api.mu.Unlock()
	if got := h.read(state); !got.IsNull() {
		t.Errorf("registry supprimée : état null attendu, reçu %v", got)
	}
}

// Import avec des UUID en majuscules : l'API les rend en minuscules.
func TestRegistryCleanupScheduleImportUppercaseIDs(t *testing.T) {
	api := &fakeCleanupAPI{}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newCleanupHarness(t, ts.URL)
	null := tftypes.NewValue(h.typ, nil)
	cfg := h.cfg(nil)
	planned, _ := h.plan(null, cfg)
	h.apply(null, planned, cfg)
	ir, err := h.srv.ImportResourceState(h.ctx, &tfprotov6.ImportResourceStateRequest{TypeName: cleanupType, ID: "REG-1/CS-1"})
	if err != nil {
		t.Fatal(err)
	}
	h.fail("import", ir.Diagnostics)
	imported := h.read(h.dec(ir.ImportedResources[0].State))
	if imported.IsNull() {
		t.Fatal("import en majuscules : planification introuvable")
	}
	if !attrOf(imported, "id").Equal(tftypes.NewValue(tftypes.String, "cs-1")) ||
		!attrOf(imported, "registry_id").Equal(tftypes.NewValue(tftypes.String, "reg-1")) {
		t.Errorf("import : UUID en minuscules attendus, reçu %v / %v", attrOf(imported, "id"), attrOf(imported, "registry_id"))
	}
	// Un état qui porte déjà l'UUID en majuscules (écrit à la main, ancien
	// import) retrouve aussi sa planification : comparaison sans casse.
	var m map[string]tftypes.Value
	_ = imported.As(&m)
	m["id"] = tftypes.NewValue(tftypes.String, "CS-1")
	if got := h.read(tftypes.NewValue(h.typ, m)); got.IsNull() {
		t.Error("état avec UUID en majuscules : planification perdue au refresh")
	}
}
