package provider

import (
	"context"
	"encoding/json"
	"fmt"
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

// terraform-provider-ccp#117 — une mise à jour en place relit la ressource et
// enregistre ce que rend l'API. Un attribut calculé que le plan promet
// (`UseStateForUnknown`) alors que la plateforme le change à l'écriture
// (`updated_at`) ou d'elle-même (`size_bytes`, statistiques d'usage) fait
// rejeter l'apply par Terraform : « Provider produced inconsistent result
// after apply ».
//
// Chaque cas passe par le vrai serveur du provider : import (refresh), plan
// d'un changement en place, apply contre une API simulée qui avance
// `updated_at` à chaque écriture, puis la vérification que fait Terraform core
// (toute valeur connue du plan doit se retrouver telle quelle dans l'état
// rendu), et enfin un plan sans diff après refresh.

// fakeAPI sert une ressource par chemin ; chaque écriture passe par `write`,
// qui la modifie et avance `updated_at`.
type fakeAPI struct {
	mu     sync.Mutex
	objs   map[string]map[string]any // chemin GET → corps
	lists  map[string][]map[string]any
	writes map[string]func(body map[string]any) (string, int)
	ticks  int
	calls  []string
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{objs: map[string]map[string]any{}, lists: map[string][]map[string]any{},
		writes: map[string]func(map[string]any) (string, int){}}
}

// bump avance l'horodatage serveur de l'objet, comme toute écriture côté API.
func (f *fakeAPI) bump(path string) {
	f.ticks++
	f.objs[path]["updated_at"] = fmt.Sprintf("2026-11-15T14:%02d:00Z", f.ticks)
}

func (f *fakeAPI) set(path, key string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[path][key] = v
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, key)
	w.Header().Set("Content-Type", "application/json")
	if fn, ok := f.writes[key]; ok {
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		path, status := fn(body)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(f.objs[path])
		return
	}
	if l, ok := f.lists[r.URL.Path]; ok && r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(l)
		return
	}
	if obj, ok := f.objs[r.URL.Path]; ok && r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(obj)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"detail":"not found: ` + key + `"}`))
}

type harness117 struct {
	t        *testing.T
	ctx      context.Context
	srv      tfprotov6.ProviderServer
	typeName string
	typ      tftypes.Object
	block    *tfprotov6.SchemaBlock
}

func newHarness117(t *testing.T, url, typeName string) *harness117 {
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
	rs := sch.ResourceSchemas[typeName]
	h := &harness117{t: t, ctx: ctx, srv: srv, typeName: typeName,
		typ: rs.ValueType().(tftypes.Object), block: rs.Block}
	h.fail("configure", cr.Diagnostics)
	return h
}

func (h *harness117) fail(step string, diags []*tfprotov6.Diagnostic) {
	h.t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			h.t.Fatalf("%s: %s — %s", step, d.Summary, d.Detail)
		}
	}
}

func (h *harness117) enc(v tftypes.Value) *tfprotov6.DynamicValue {
	h.t.Helper()
	d, err := tfprotov6.NewDynamicValue(h.typ, v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func (h *harness117) dec(d *tfprotov6.DynamicValue) tftypes.Value {
	h.t.Helper()
	v, err := d.Unmarshal(h.typ)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func attrsOf(v tftypes.Value) map[string]tftypes.Value {
	var m map[string]tftypes.Value
	_ = v.As(&m)
	return m
}

// imported is the state `terraform import <id>` produces: Read from an id alone.
func (h *harness117) imported(id string) tftypes.Value {
	h.t.Helper()
	m := map[string]tftypes.Value{}
	for k, at := range h.typ.AttributeTypes {
		m[k] = tftypes.NewValue(at, nil)
	}
	m["id"] = tftypes.NewValue(tftypes.String, id)
	return h.read(tftypes.NewValue(h.typ, m))
}

func (h *harness117) read(state tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: h.typeName, CurrentState: h.enc(state)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("read", r.Diagnostics)
	return h.dec(r.NewState)
}

// configFrom is the configuration that matches `state`: every attribute the
// practitioner can write keeps its value, computed-only attributes are null,
// then `set` overrides.
func (h *harness117) configFrom(state tftypes.Value, set map[string]tftypes.Value) tftypes.Value {
	h.t.Helper()
	st := attrsOf(state)
	m := map[string]tftypes.Value{}
	for _, a := range h.block.Attributes {
		if a.Computed && !a.Optional && !a.Required {
			m[a.Name] = tftypes.NewValue(h.typ.AttributeTypes[a.Name], nil)
			continue
		}
		m[a.Name] = st[a.Name]
	}
	for _, b := range h.block.BlockTypes {
		m[b.TypeName] = st[b.TypeName]
	}
	for k, v := range set {
		m[k] = v
	}
	return tftypes.NewValue(h.typ, m)
}

// unset leaves attributes out of the configuration (null), as a practitioner
// who never wrote them: an Optional+Computed attribute then comes from the state.
func unset(cfg tftypes.Value, names ...string) tftypes.Value {
	m := attrsOf(cfg)
	for _, n := range names {
		m[n] = tftypes.NewValue(m[n].Type(), nil)
	}
	return tftypes.NewValue(cfg.Type(), m)
}

// proposed mimics Terraform core: configured values, the prior value where a
// computed attribute is left null in the configuration (also inside a single
// nested block present in both).
func proposedValue(block *tfprotov6.SchemaBlock, typ tftypes.Object, prior, cfg tftypes.Value) tftypes.Value {
	if prior.IsNull() || cfg.IsNull() {
		return cfg
	}
	p, c := attrsOf(prior), attrsOf(cfg)
	out := map[string]tftypes.Value{}
	for _, a := range block.Attributes {
		if a.Computed && c[a.Name].IsNull() {
			out[a.Name] = p[a.Name]
		} else {
			out[a.Name] = c[a.Name]
		}
	}
	for _, b := range block.BlockTypes {
		if b.Nesting == tfprotov6.SchemaNestedBlockNestingModeSingle {
			out[b.TypeName] = proposedValue(b.Block, typ.AttributeTypes[b.TypeName].(tftypes.Object), p[b.TypeName], c[b.TypeName])
		} else {
			out[b.TypeName] = c[b.TypeName]
		}
	}
	return tftypes.NewValue(typ, out)
}

func (h *harness117) plan(prior, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: h.typeName,
		PriorState: h.enc(prior), ProposedNewState: h.enc(proposedValue(h.block, h.typ, prior, cfg)), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("plan", r.Diagnostics)
	if len(r.RequiresReplace) != 0 {
		h.t.Fatalf("plan : remplacement planifié %v, mise à jour en place attendue", r.RequiresReplace)
	}
	return h.dec(r.PlannedState)
}

func (h *harness117) apply(prior, planned, cfg tftypes.Value) tftypes.Value {
	h.t.Helper()
	r, err := h.srv.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: h.typeName,
		PriorState: h.enc(prior), PlannedState: h.enc(planned), Config: h.enc(cfg)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.fail("apply", r.Diagnostics)
	return h.dec(r.NewState)
}

// inconsistencies is Terraform core's check after apply: every value known in
// the plan must be in the new state as is; unknown ones may become anything.
func inconsistencies(at string, planned, got tftypes.Value) []string {
	if !planned.IsKnown() {
		return nil
	}
	if planned.IsNull() || got.IsNull() || !got.IsKnown() {
		if !planned.Equal(got) {
			return []string{fmt.Sprintf("%s: planifié %v, rendu %v", at, planned, got)}
		}
		return nil
	}
	switch planned.Type().(type) {
	case tftypes.Object:
		var out []string
		g := attrsOf(got)
		for k, v := range attrsOf(planned) {
			out = append(out, inconsistencies(at+"."+k, v, g[k])...)
		}
		return out
	case tftypes.List, tftypes.Set:
		var p, g []tftypes.Value
		_ = planned.As(&p)
		_ = got.As(&g)
		if len(p) != len(g) {
			return []string{fmt.Sprintf("%s: %d éléments planifiés, %d rendus", at, len(p), len(g))}
		}
		var out []string
		for i := range p {
			out = append(out, inconsistencies(fmt.Sprintf("%s[%d]", at, i), p[i], g[i])...)
		}
		return out
	}
	if !planned.Equal(got) {
		return []string{fmt.Sprintf("%s: planifié %v, rendu %v", at, planned, got)}
	}
	return nil
}

// updateInPlace runs plan → (drift) → apply → core check → refresh → plan.
func (h *harness117) updateInPlace(state, cfg tftypes.Value, drift func()) tftypes.Value {
	h.t.Helper()
	planned := h.plan(state, cfg)
	if drift != nil {
		drift() // la plateforme bouge entre le plan et l'apply
	}
	got := h.apply(state, planned, cfg)
	for _, e := range inconsistencies(h.typeName, planned, got) {
		h.t.Errorf("Provider produced inconsistent result after apply — %s", e)
	}
	refreshed := h.read(got)
	if again := h.plan(refreshed, cfg); !again.Equal(refreshed) {
		diff, _ := again.Diff(refreshed)
		h.t.Errorf("diff après apply : %v", diff)
	}
	return got
}

func str117(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
func num117(n int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, n) }
func bool117(b bool) tftypes.Value  { return tftypes.NewValue(tftypes.Bool, b) }
func assertCalled(t *testing.T, api *fakeAPI, want string) {
	t.Helper()
	for _, c := range api.calls {
		if c == want {
			return
		}
	}
	t.Errorf("appel %q attendu, reçus %s", want, strings.Join(api.calls, ", "))
}

// ── ccp_object_bucket ───────────────────────────────────────────────────────

func bucketAPI117() *fakeAPI {
	api := newFakeAPI()
	const p = "/v1/buckets/b-1"
	api.objs[p] = map[string]any{
		"id": "b-1", "name": "acme-docs", "region": "RNN", "endpoint_url": "https://s3.example",
		"size_bytes": 1000, "status": "active", "is_public": false, "error_message": nil, "tags": []string{},
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.objs[p+"/credentials"] = map[string]any{"access_key": "AK", "secret_key": "SK", "endpoint_url": "https://s3.example"}
	api.objs[p+"/versioning"] = map[string]any{"status": nil, "noncurrent_version_expiration_days": nil, "sync_status": "none"}
	api.writes["PUT "+p+"/versioning"] = func(b map[string]any) (string, int) {
		days := b["noncurrent_version_expiration_days"]
		if days == nil {
			days = 30
		}
		api.objs[p+"/versioning"] = map[string]any{"status": b["status"], "noncurrent_version_expiration_days": days, "sync_status": "applied"}
		api.bump(p) // la plateforme écrit la ligne du bucket
		return p + "/versioning", http.StatusOK
	}
	api.writes["PATCH "+p] = func(b map[string]any) (string, int) {
		if v, ok := b["is_public"]; ok {
			api.objs[p]["is_public"] = v
		}
		api.bump(p)
		return p, http.StatusOK
	}
	return api
}

// Le cas de l'issue : bloc `versioning` ajouté à un bucket existant.
func TestObjectBucketAddVersioningIsConsistent_117(t *testing.T) {
	api := bucketAPI117()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_object_bucket")
	state := h.imported("b-1")

	vt := h.typ.AttributeTypes["versioning"].(tftypes.Object)
	cfg := h.configFrom(state, map[string]tftypes.Value{"versioning": tftypes.NewValue(vt, map[string]tftypes.Value{
		"status": str117("Enabled"), "noncurrent_version_expiration_days": num117(30),
	})})
	h.updateInPlace(state, cfg, nil)
	assertCalled(t, api, "PUT /v1/buckets/b-1/versioning")
}

// Visibilité changée pendant que les statistiques d'usage avancent : la taille
// rendue n'est pas celle du dernier refresh.
func TestObjectBucketVisibilityWhileUsageMoves_117(t *testing.T) {
	api := bucketAPI117()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_object_bucket")
	state := h.imported("b-1")

	cfg := h.configFrom(state, map[string]tftypes.Value{"is_public": bool117(true)})
	h.updateInPlace(state, cfg, func() { api.set("/v1/buckets/b-1", "size_bytes", 5000) })
	assertCalled(t, api, "PATCH /v1/buckets/b-1")
}

// Sans changement, rien n'est « known after apply » : pas de diff perpétuel.
func TestObjectBucketNoChangeNoDiff_117(t *testing.T) {
	api := bucketAPI117()
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_object_bucket")
	state := h.imported("b-1")
	if planned := h.plan(state, h.configFrom(state, nil)); !planned.Equal(state) {
		diff, _ := planned.Diff(state)
		t.Fatalf("diff sans changement : %v", diff)
	}
}

// ── ccp_block_volume ────────────────────────────────────────────────────────

func TestBlockVolumeResizeIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/volumes/v-1"
	api.objs[p] = map[string]any{
		"id": "v-1", "name": "data", "region": "RNN", "size_gb": 10, "status": "available",
		"attached_to_id": nil, "attached_to_type": nil, "attached_to_name": nil, "error_message": nil,
		"tags": []string{}, "created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.writes["POST "+p+"/resize"] = func(b map[string]any) (string, int) {
		api.objs[p]["size_gb"] = b["size_gb"]
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_block_volume")
	state := h.imported("v-1")

	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"size_gb": num117(20)}), nil)
	assertCalled(t, api, "POST /v1/volumes/v-1/resize")
}

// ── ccp_vm_instance ─────────────────────────────────────────────────────────

func TestVMInstanceRenameIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/vm-instances/vm-1"
	api.objs[p] = map[string]any{
		"id": "vm-1", "name": "web", "region": "RNN", "plan": "s-1", "cores": 1, "memory_mb": 1024,
		"disk_gb": 20, "template": "debian-12", "status": "running", "ip_address": "10.0.0.5",
		"public_ip_address": nil, "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{}, "os_family": "linux",
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.writes["PATCH "+p] = func(b map[string]any) (string, int) {
		if v, ok := b["name"]; ok {
			api.objs[p]["name"] = v
		}
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_vm_instance")
	state := h.imported("vm-1")

	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"name": str117("web-2")}), nil)
	assertCalled(t, api, "PATCH /v1/vm-instances/vm-1")
}

// IP publique attachée : l'adresse rendue n'est pas celle du dernier refresh.
func TestVMInstanceAttachPublicIPIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/vm-instances/vm-1"
	api.objs[p] = map[string]any{
		"id": "vm-1", "name": "web", "region": "RNN", "plan": "s-1", "cores": 1, "memory_mb": 1024,
		"disk_gb": 20, "template": "debian-12", "status": "running", "ip_address": "10.0.0.5",
		"public_ip_address": nil, "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{}, "os_family": "linux",
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.lists["/v1/public-ips"] = []map[string]any{{"id": "ip-1", "region": "RNN", "ip_address": "203.0.113.7", "status": "allocated"}}
	api.objs["/v1/public-ips/ip-1"] = api.lists["/v1/public-ips"][0]
	api.writes["POST /v1/public-ips/ip-1/attach"] = func(b map[string]any) (string, int) {
		api.lists["/v1/public-ips"][0]["vm_instance_id"] = "vm-1"
		api.lists["/v1/public-ips"][0]["status"] = "attached"
		api.objs[p]["public_ip_address"] = "203.0.113.7"
		api.bump(p)
		return "/v1/public-ips/ip-1", http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_vm_instance")
	state := h.imported("vm-1")

	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"public_ip_id": str117("ip-1")}), nil)
	assertCalled(t, api, "POST /v1/public-ips/ip-1/attach")
}

// IP publique échangée : l'adresse connue de l'état est remplacée.
func TestVMInstanceSwapPublicIPIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/vm-instances/vm-1"
	api.objs[p] = map[string]any{
		"id": "vm-1", "name": "web", "region": "RNN", "plan": "s-1", "cores": 1, "memory_mb": 1024,
		"disk_gb": 20, "template": "debian-12", "status": "running", "ip_address": "10.0.0.5",
		"public_ip_address": "203.0.113.7", "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{}, "os_family": "linux",
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	ips := []map[string]any{
		{"id": "ip-1", "region": "RNN", "ip_address": "203.0.113.7", "status": "attached", "vm_instance_id": "vm-1"},
		{"id": "ip-2", "region": "RNN", "ip_address": "203.0.113.8", "status": "allocated"},
	}
	api.lists["/v1/public-ips"] = ips
	api.objs["/v1/public-ips/ip-1"], api.objs["/v1/public-ips/ip-2"] = ips[0], ips[1]
	api.writes["POST /v1/public-ips/ip-1/detach"] = func(map[string]any) (string, int) {
		delete(ips[0], "vm_instance_id")
		ips[0]["status"] = "allocated"
		api.objs[p]["public_ip_address"] = nil
		api.bump(p)
		return "/v1/public-ips/ip-1", http.StatusOK
	}
	api.writes["POST /v1/public-ips/ip-2/attach"] = func(map[string]any) (string, int) {
		ips[1]["vm_instance_id"], ips[1]["status"] = "vm-1", "attached"
		api.objs[p]["public_ip_address"] = "203.0.113.8"
		api.bump(p)
		return "/v1/public-ips/ip-2", http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_vm_instance")
	state := h.imported("vm-1")

	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"public_ip_id": str117("ip-2")}), nil)
	assertCalled(t, api, "POST /v1/public-ips/ip-2/attach")
}

// ── ccp_container_instance ──────────────────────────────────────────────────

func TestContainerInstanceDiskResizeIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/containers/ct-1"
	api.objs[p] = map[string]any{
		"id": "ct-1", "name": "app", "region": "RNN", "plan": "c-1", "cores": 1, "memory_mb": 512,
		"disk_gb": 8, "template": "debian-12", "status": "running", "ip_address": "10.0.0.6",
		"public_ip_address": nil, "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{},
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.writes["POST "+p+"/resize-disk"] = func(b map[string]any) (string, int) {
		api.objs[p]["disk_gb"] = b["disk_gb"]
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_container_instance")
	state := h.imported("ct-1")

	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"disk_gb": num117(16)}), nil)
	assertCalled(t, api, "POST /v1/containers/ct-1/resize-disk")
}

// Échange d'IP publique sur un conteneur : même contrat que la VM.
func TestContainerInstanceSwapPublicIPIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/containers/ct-1"
	api.objs[p] = map[string]any{
		"id": "ct-1", "name": "app", "region": "RNN", "plan": "c-1", "cores": 1, "memory_mb": 512,
		"disk_gb": 8, "template": "debian-12", "status": "running", "ip_address": "10.0.0.6",
		"public_ip_address": "203.0.113.7", "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{},
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	ips := []map[string]any{
		{"id": "ip-1", "region": "RNN", "ip_address": "203.0.113.7", "status": "attached", "container_id": "ct-1"},
		{"id": "ip-2", "region": "RNN", "ip_address": "203.0.113.8", "status": "allocated"},
	}
	api.lists["/v1/public-ips"] = ips
	api.objs["/v1/public-ips/ip-1"], api.objs["/v1/public-ips/ip-2"] = ips[0], ips[1]
	api.writes["POST /v1/public-ips/ip-1/detach"] = func(map[string]any) (string, int) {
		delete(ips[0], "container_id")
		ips[0]["status"] = "allocated"
		api.objs[p]["public_ip_address"] = nil
		api.bump(p)
		return "/v1/public-ips/ip-1", http.StatusOK
	}
	api.writes["POST /v1/public-ips/ip-2/attach"] = func(map[string]any) (string, int) {
		ips[1]["container_id"], ips[1]["status"] = "ct-1", "attached"
		api.objs[p]["public_ip_address"] = "203.0.113.8"
		api.bump(p)
		return "/v1/public-ips/ip-2", http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_container_instance")
	state := h.imported("ct-1")

	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"public_ip_id": str117("ip-2")}), nil)
	assertCalled(t, api, "POST /v1/public-ips/ip-2/attach")
}

// Renommer une VM qui a une IP publique ne rend pas l'adresse inconnue : elle
// ne bouge que si `public_ip_id` change.
func TestVMInstanceRenameKeepsPublicIPAddressKnown_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/vm-instances/vm-1"
	api.objs[p] = map[string]any{
		"id": "vm-1", "name": "web", "region": "RNN", "plan": "s-1", "cores": 1, "memory_mb": 1024,
		"disk_gb": 20, "template": "debian-12", "status": "running", "ip_address": "10.0.0.5",
		"public_ip_address": "203.0.113.7", "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{}, "os_family": "linux",
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.lists["/v1/public-ips"] = []map[string]any{{"id": "ip-1", "region": "RNN", "ip_address": "203.0.113.7",
		"status": "attached", "vm_instance_id": "vm-1"}}
	api.writes["PATCH "+p] = func(b map[string]any) (string, int) {
		api.objs[p]["name"] = b["name"]
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_vm_instance")
	state := h.imported("vm-1")

	for name, cfg := range map[string]tftypes.Value{
		"public_ip_id écrit":  h.configFrom(state, map[string]tftypes.Value{"name": str117("web-2")}),
		"public_ip_id absent": unset(h.configFrom(state, map[string]tftypes.Value{"name": str117("web-2")}), "public_ip_id", "disk_gb", "tags"),
	} {
		if got := attrsOf(h.plan(state, cfg))["public_ip_address"]; !got.Equal(str117("203.0.113.7")) {
			t.Errorf("renommage (%s) : public_ip_address doit rester connue, planifiée %v", name, got)
		}
	}
	h.updateInPlace(state, unset(h.configFrom(state, map[string]tftypes.Value{"name": str117("web-2")}), "public_ip_id", "disk_gb", "tags"), nil)
}

// Sans changement, aucune ressource ne montre de diff (rien de « known after
// apply » sans mise à jour) : le retrait de UseStateForUnknown ne crée pas de
// diff perpétuel.
func TestNoChangeNoDiff_117(t *testing.T) {
	for _, c := range []struct{ typeName, id, path string }{
		{"ccp_block_volume", "v-1", "/v1/volumes/v-1"},
		{"ccp_vm_instance", "vm-1", "/v1/vm-instances/vm-1"},
		{"ccp_container_instance", "ct-1", "/v1/containers/ct-1"},
	} {
		t.Run(c.typeName, func(t *testing.T) {
			api := newFakeAPI()
			api.objs[c.path] = map[string]any{
				"id": c.id, "name": "x", "region": "RNN", "plan": "s-1", "cores": 1, "memory_mb": 512,
				"disk_gb": 8, "size_gb": 10, "template": "debian-12", "status": "running",
				"ip_address": "10.0.0.6", "public_ip_address": "203.0.113.7", "vnet_id": "vn-1",
				"has_root_password": false, "tags": []string{}, "os_family": "linux",
				"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
			}
			api.lists["/v1/public-ips"] = []map[string]any{}
			ts := httptest.NewServer(api)
			defer ts.Close()
			h := newHarness117(t, ts.URL, c.typeName)
			state := h.imported(c.id)
			if planned := h.plan(state, h.configFrom(state, nil)); !planned.Equal(state) {
				diff, _ := planned.Diff(state)
				t.Fatalf("diff sans changement : %v", diff)
			}
		})
	}
}

// Volume attaché puis détaché : `status` et `attached_to_name` suivent
// l'attachement, ils ne peuvent pas rester ceux de l'état au plan.
func TestBlockVolumeAttachDetachIsConsistent_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/volumes/v-1"
	api.objs[p] = map[string]any{
		"id": "v-1", "name": "data", "region": "RNN", "size_gb": 10, "status": "available",
		"attached_to_id": nil, "attached_to_type": nil, "attached_to_name": nil, "error_message": nil,
		"tags": []string{}, "created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.writes["POST "+p+"/attach"] = func(b map[string]any) (string, int) {
		api.objs[p]["status"], api.objs[p]["attached_to_name"] = "attached", "web"
		api.objs[p]["attached_to_id"], api.objs[p]["attached_to_type"] = b["resource_id"], b["resource_type"]
		api.bump(p)
		return p, http.StatusOK
	}
	api.writes["POST "+p+"/detach"] = func(map[string]any) (string, int) {
		api.objs[p]["status"], api.objs[p]["attached_to_name"] = "available", nil
		api.objs[p]["attached_to_id"], api.objs[p]["attached_to_type"] = nil, nil
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_block_volume")
	state := h.imported("v-1")

	const vm = "11111111-1111-1111-1111-111111111111"
	state = h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{
		"attached_to_id": str117(vm), "attached_to_type": str117("vm"),
	}), nil)
	assertCalled(t, api, "POST /v1/volumes/v-1/attach")
	if got := attrsOf(state)["status"]; !got.Equal(str117("attached")) {
		t.Fatalf("après attachement : status attached attendu, reçu %v", got)
	}
	null := tftypes.NewValue(tftypes.String, nil)
	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"attached_to_id": null, "attached_to_type": null}), nil)
	assertCalled(t, api, "POST /v1/volumes/v-1/detach")
}

// Disque agrandi alors que la plateforme rend un statut transitoire : le
// statut ne peut pas rester celui de l'état au plan.
func TestVMInstanceDiskResizeTransitionalStatus_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/vm-instances/vm-1"
	api.objs[p] = map[string]any{
		"id": "vm-1", "name": "web", "region": "RNN", "plan": "s-1", "cores": 1, "memory_mb": 1024,
		"disk_gb": 20, "template": "debian-12", "status": "running", "ip_address": "10.0.0.5",
		"public_ip_address": nil, "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{}, "os_family": "linux",
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.lists["/v1/public-ips"] = []map[string]any{}
	api.writes["POST "+p+"/resize-disk"] = func(b map[string]any) (string, int) {
		api.objs[p]["disk_gb"], api.objs[p]["status"] = b["disk_gb"], "resizing"
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_vm_instance")
	state := h.imported("vm-1")
	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"disk_gb": num117(40)}), nil)
}

func TestContainerInstanceDiskResizeTransitionalStatus_117(t *testing.T) {
	api := newFakeAPI()
	const p = "/v1/containers/ct-1"
	api.objs[p] = map[string]any{
		"id": "ct-1", "name": "app", "region": "RNN", "plan": "c-1", "cores": 1, "memory_mb": 512,
		"disk_gb": 8, "template": "debian-12", "status": "running", "ip_address": "10.0.0.6",
		"public_ip_address": nil, "vnet_id": "vn-1", "scale_set_id": nil, "user_data": nil,
		"error_message": nil, "has_root_password": false, "tags": []string{},
		"created_at": "2026-11-06T13:54:28Z", "updated_at": "2026-11-06T13:54:28Z",
	}
	api.lists["/v1/public-ips"] = []map[string]any{}
	api.writes["POST "+p+"/resize-disk"] = func(b map[string]any) (string, int) {
		api.objs[p]["disk_gb"], api.objs[p]["status"] = b["disk_gb"], "resizing"
		api.bump(p)
		return p, http.StatusOK
	}
	ts := httptest.NewServer(api)
	defer ts.Close()
	h := newHarness117(t, ts.URL, "ccp_container_instance")
	state := h.imported("ct-1")
	h.updateInPlace(state, h.configFrom(state, map[string]tftypes.Value{"disk_gb": num117(16)}), nil)
}
