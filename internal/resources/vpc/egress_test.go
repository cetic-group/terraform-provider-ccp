package vpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// Sortie internet dédiée d'un VPC (cetic-cloud-platform#1734).

const vpcID = "vpc-1734"

// vpcBody est la réponse VPC de l'API. `status` vide = aucune IP liée
// (les trois champs egress_* à null).
func vpcBody(dedicated bool, status string) map[string]any {
	b := map[string]any{
		"id": vpcID, "name": "prod", "region": "RNN", "cidr": "10.1.0.0/16",
		"status": "active", "tags": []string{}, "created_at": "2026-11-02T10:00:00Z",
		"dedicated_egress_ip": dedicated,
		"egress_ip":           nil, "egress_ip_type": nil, "egress_ip_status": nil,
	}
	if status != "" {
		b["egress_ip"] = "203.0.113.10"
		b["egress_ip_type"] = "edge"
		b["egress_ip_status"] = status
	}
	return b
}

// rapide raccourcit l'attente de la sortie le temps du test.
func rapide(t *testing.T) {
	t.Helper()
	interval, timeout := egressPollInterval, egressPollTimeout
	egressPollInterval, egressPollTimeout = time.Millisecond, 2*time.Second
	t.Cleanup(func() { egressPollInterval, egressPollTimeout = interval, timeout })
}

func newResource(t *testing.T, routes testutil.Routes) (*vpcResource, *testutil.Server, schema.Schema) {
	t.Helper()
	srv := testutil.NewTestServer(t, routes)
	t.Cleanup(srv.Close)
	r := &vpcResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sch resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sch)
	return r, srv, sch.Schema
}

// planCreate est le plan d'une création : les attributs calculés sont inconnus.
func planCreate(dedicated bool) vpcResourceModel {
	u := types.StringUnknown()
	return vpcResourceModel{
		ID: u, Name: types.StringValue("prod"), Region: types.StringValue("RNN"),
		Cidr: types.StringValue("10.1.0.0/16"), Tags: types.ListValueMust(types.StringType, nil),
		Status: u, CreatedAt: u,
		DedicatedEgressIP: types.BoolValue(dedicated),
		EgressIP:          u, EgressIPType: u, EgressIPStatus: u,
	}
}

// stateFrom est l'état d'un VPC existant tel que l'API le décrit.
func stateFrom(t *testing.T, body map[string]any) vpcResourceModel {
	t.Helper()
	raw, _ := json.Marshal(body)
	var v client.VPC
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	m := planCreate(false)
	if d := applyVPCToModel(context.Background(), &v, &m); d.HasError() {
		t.Fatalf("applyVPCToModel: %v", d)
	}
	return m
}

func toState(t *testing.T, sch schema.Schema, m vpcResourceModel) tfsdk.State {
	t.Helper()
	st := tfsdk.State{Schema: sch}
	if d := st.Set(context.Background(), &m); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return st
}

func runCreate(t *testing.T, routes testutil.Routes, plan vpcResourceModel) (*testutil.Server, vpcResourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	r, srv, sch := newResource(t, routes)
	pl := toState(t, sch, plan)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sch}}, resp)
	var out vpcResourceModel
	if !resp.State.Raw.IsNull() {
		resp.State.Get(ctx, &out)
	}
	return srv, out, resp.Diagnostics
}

func runUpdate(t *testing.T, routes testutil.Routes, prior, plan vpcResourceModel) (*testutil.Server, vpcResourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	r, srv, sch := newResource(t, routes)
	st := toState(t, sch, prior)
	pl := toState(t, sch, plan)
	resp := &resource.UpdateResponse{State: st}
	r.Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sch}, State: st,
	}, resp)
	var out vpcResourceModel
	resp.State.Get(ctx, &out)
	return srv, out, resp.Diagnostics
}

// sentBody rend le corps JSON de l'unique requête `method` reçue.
func sentBody(t *testing.T, srv *testutil.Server, method string) map[string]any {
	t.Helper()
	var out map[string]any
	n := 0
	for _, c := range srv.Calls() {
		if c.Method == method {
			n++
			if err := json.Unmarshal(c.Body, &out); err != nil {
				t.Fatalf("corps %s illisible : %v", method, err)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d requêtes %s, 1 attendue", n, method)
	}
	return out
}

func assertNoPending(t *testing.T, srv *testutil.Server) {
	t.Helper()
	if n := srv.PendingRoutes(); n != 0 {
		t.Fatalf("%d réponses de l'API jamais consommées", n)
	}
}

// ── Défaut false ────────────────────────────────────────────────────────────

// Le défaut doit être un `false` EXPLICITE : avec Optional+Computed sans défaut,
// retirer l'attribut de la configuration garderait `true` dans l'état pour
// toujours, et la sortie ne serait jamais retirée.
func TestDedicatedEgressIP_DefaultFalse(t *testing.T) {
	_, _, sch := newResource(t, nil)
	attr, ok := sch.Attributes["dedicated_egress_ip"].(schema.BoolAttribute)
	if !ok {
		t.Fatal("dedicated_egress_ip absent ou pas un booléen")
	}
	if !attr.Optional || attr.Default == nil {
		t.Fatalf("Optional=%v Default=%v : un défaut explicite est requis", attr.Optional, attr.Default)
	}
	var resp defaults.BoolResponse
	attr.Default.DefaultBool(context.Background(), defaults.BoolRequest{}, &resp)
	if resp.PlanValue.IsNull() || resp.PlanValue.ValueBool() {
		t.Fatalf("défaut = %v, false attendu", resp.PlanValue)
	}
	for _, name := range []string{"egress_ip", "egress_ip_type", "egress_ip_status"} {
		a := sch.Attributes[name].(schema.StringAttribute)
		if !a.Computed || a.Optional || a.Required {
			t.Errorf("%s doit être Computed seul", name)
		}
	}
}

// Sans sortie dédiée, le POST n'envoie pas le champ, rien n'est attendu et
// l'état porte false avec des egress_* à null.
func TestCreate_WithoutDedicatedEgress(t *testing.T) {
	rapide(t)
	srv, out, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusCreated, Body: vpcBody(false, "")},
	}, planCreate(false))
	if diags.HasError() {
		t.Fatalf("create : %v", diags)
	}
	if v, ok := sentBody(t, srv, "POST")["dedicated_egress_ip"]; ok {
		t.Fatalf("dedicated_egress_ip=false ne doit pas être envoyé, reçu %v", v)
	}
	if out.DedicatedEgressIP.ValueBool() || !out.EgressIP.IsNull() || !out.EgressIPStatus.IsNull() {
		t.Fatalf("état inattendu : %+v", out)
	}
	assertNoPending(t, srv)
}

// ── Activation ──────────────────────────────────────────────────────────────

// À la création avec true, le provider envoie le champ puis attend `attached`.
func TestCreate_DedicatedEgressWaitsForAttached(t *testing.T) {
	rapide(t)
	path := "/v1/vpcs/" + vpcID
	srv, out, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusCreated, Body: vpcBody(true, "attaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(true, "attaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(true, "attached")},
	}, planCreate(true))
	if diags.HasError() || diags.WarningsCount() != 0 {
		t.Fatalf("create : %v", diags)
	}
	if sentBody(t, srv, "POST")["dedicated_egress_ip"] != true {
		t.Fatal("dedicated_egress_ip=true non envoyé au POST")
	}
	if out.EgressIPStatus.ValueString() != "attached" || out.EgressIP.ValueString() != "203.0.113.10" ||
		out.EgressIPType.ValueString() != "edge" || !out.DedicatedEgressIP.ValueBool() {
		t.Fatalf("état inattendu : %+v", out)
	}
	assertNoPending(t, srv)
}

// Un VPC tout juste créé n'a pas de VNet snat=true : l'IP reste `allocated`
// (réservée) et le serait jusqu'au timeout. `allocated` termine l'attente.
func TestCreate_DedicatedEgressAllocatedIsSettled(t *testing.T) {
	rapide(t)
	srv, out, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusCreated, Body: vpcBody(true, "allocated")},
		{Method: "GET", Path: "/v1/vpcs/" + vpcID, Status: http.StatusOK, Body: vpcBody(true, "allocated")},
	}, planCreate(true))
	if diags.HasError() || diags.WarningsCount() != 0 {
		t.Fatalf("create : %v", diags)
	}
	if out.EgressIPStatus.ValueString() != "allocated" {
		t.Fatalf("statut = %v", out.EgressIPStatus)
	}
	assertNoPending(t, srv)
}

// Une IP en erreur à la création : avertissement, pas erreur. Une erreur
// marquerait le VPC « tainted » et le prochain apply le remplacerait, VNets
// comprises, pour une IP que la plateforme réessaie seule.
func TestCreate_DedicatedEgressErrorIsWarning(t *testing.T) {
	rapide(t)
	_, out, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusCreated, Body: vpcBody(true, "attaching")},
		{Method: "GET", Path: "/v1/vpcs/" + vpcID, Status: http.StatusOK, Body: vpcBody(true, "error")},
	}, planCreate(true))
	if diags.HasError() {
		t.Fatalf("une IP en erreur ne doit pas faire échouer la création : %v", diags)
	}
	if diags.WarningsCount() != 1 {
		t.Fatalf("un avertissement attendu, reçu %v", diags)
	}
	if out.ID.ValueString() != vpcID || out.EgressIPStatus.ValueString() != "error" {
		t.Fatalf("l'état doit garder le VPC et le statut réel : %+v", out)
	}
}

// Une API antérieure à #1734 ignore le champ : erreur explicite plutôt qu'un
// « inconsistent result » du framework.
func TestCreate_PlatformIgnoringDedicatedEgressFailsLoudly(t *testing.T) {
	rapide(t)
	_, out, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusCreated, Body: vpcBody(false, "")},
	}, planCreate(true))
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Summary(), "not supported") {
		t.Fatalf("erreur attendue, reçu %v", diags)
	}
	if out.ID.ValueString() != vpcID {
		t.Fatal("le VPC créé doit rester dans l'état")
	}
}

// PATCH true puis attente de `attached`.
func TestUpdate_EnableWaitsForAttached(t *testing.T) {
	rapide(t)
	path := "/v1/vpcs/" + vpcID
	prior := stateFrom(t, vpcBody(false, ""))
	plan := prior
	plan.DedicatedEgressIP = types.BoolValue(true)
	plan.EgressIP, plan.EgressIPType, plan.EgressIPStatus = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()

	srv, out, diags := runUpdate(t, testutil.Routes{
		{Method: "PATCH", Path: path, Status: http.StatusOK, Body: vpcBody(true, "attaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(true, "attaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(true, "attached")},
	}, prior, plan)
	if diags.HasError() {
		t.Fatalf("update : %v", diags)
	}
	body := sentBody(t, srv, "PATCH")
	if len(body) != 1 || body["dedicated_egress_ip"] != true {
		t.Fatalf("corps PATCH = %v, {dedicated_egress_ip: true} attendu", body)
	}
	if out.EgressIPStatus.ValueString() != "attached" || out.EgressIP.ValueString() != "203.0.113.10" {
		t.Fatalf("état inattendu : %+v", out)
	}
	assertNoPending(t, srv)
}

// ── Désactivation ───────────────────────────────────────────────────────────

// PATCH false (envoyé explicitement, pas omis) puis attente que l'IP ne soit
// plus liée : `detaching` garde les champs remplis jusqu'au retrait prouvé.
func TestUpdate_DisableWaitsUntilReleased(t *testing.T) {
	rapide(t)
	path := "/v1/vpcs/" + vpcID
	prior := stateFrom(t, vpcBody(true, "attached"))
	plan := prior
	plan.DedicatedEgressIP = types.BoolValue(false)
	plan.EgressIP, plan.EgressIPType, plan.EgressIPStatus = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()

	srv, out, diags := runUpdate(t, testutil.Routes{
		{Method: "PATCH", Path: path, Status: http.StatusOK, Body: vpcBody(false, "detaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(false, "detaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(false, "")},
	}, prior, plan)
	if diags.HasError() {
		t.Fatalf("update : %v", diags)
	}
	if v, ok := sentBody(t, srv, "PATCH")["dedicated_egress_ip"]; !ok || v != false {
		t.Fatalf("dedicated_egress_ip=false doit être envoyé, reçu %v (présent=%v)", v, ok)
	}
	if out.DedicatedEgressIP.ValueBool() || !out.EgressIP.IsNull() || !out.EgressIPType.IsNull() || !out.EgressIPStatus.IsNull() {
		t.Fatalf("après retrait, egress_* doivent être null : %+v", out)
	}
	assertNoPending(t, srv)
}

// Une IP qui passe en `error` pendant la pose : diagnostic d'erreur, et l'état
// enregistre le statut réel.
func TestUpdate_ErrorStatusFails(t *testing.T) {
	rapide(t)
	path := "/v1/vpcs/" + vpcID
	prior := stateFrom(t, vpcBody(false, ""))
	plan := prior
	plan.DedicatedEgressIP = types.BoolValue(true)
	plan.EgressIP, plan.EgressIPType, plan.EgressIPStatus = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()

	_, out, diags := runUpdate(t, testutil.Routes{
		{Method: "PATCH", Path: path, Status: http.StatusOK, Body: vpcBody(true, "attaching")},
		{Method: "GET", Path: path, Status: http.StatusOK, Body: vpcBody(true, "error")},
	}, prior, plan)
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "`error`") {
		t.Fatalf("erreur attendue sur le statut `error`, reçu %v", diags)
	}
	if out.EgressIPStatus.ValueString() != "error" {
		t.Fatalf("l'état doit porter le statut réel : %v", out.EgressIPStatus)
	}
}

// ── Erreurs de l'API propagées ──────────────────────────────────────────────

func TestUpdate_APIRefusalPropagated(t *testing.T) {
	cases := []struct {
		name   string
		status int
		detail string
	}{
		{"409 aucune IP", http.StatusConflict, "Aucune IP publique disponible pour la sortie dédiée dans la région RNN"},
		{"409 retrait en cours", http.StatusConflict, "La sortie dédiée précédente est en cours de retrait : réessayez dans quelques minutes."},
		{"422 non proposée", http.StatusUnprocessableEntity, "La sortie internet par une IP publique dédiée n'est pas proposée sur cette plateforme."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rapide(t)
			prior := stateFrom(t, vpcBody(false, ""))
			plan := prior
			plan.DedicatedEgressIP = types.BoolValue(true)
			plan.EgressIP, plan.EgressIPType, plan.EgressIPStatus = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
			srv, out, diags := runUpdate(t, testutil.Routes{
				{Method: "PATCH", Path: "/v1/vpcs/" + vpcID, Status: c.status, Body: map[string]any{"detail": c.detail}},
			}, prior, plan)
			if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), c.detail) {
				t.Fatalf("le détail de l'API doit être repris tel quel, reçu %v", diags)
			}
			// Refus : l'état reste celui d'avant (sortie non activée).
			if out.DedicatedEgressIP.ValueBool() {
				t.Fatal("un refus ne doit pas enregistrer dedicated_egress_ip=true")
			}
			assertNoPending(t, srv)
		})
	}
}

// Le 429 de quota porte un `detail` objet : son `message` est repris.
func TestCreate_QuotaRefusalPropagated(t *testing.T) {
	rapide(t)
	_, out, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusTooManyRequests, Body: map[string]any{
			"detail": map[string]any{"code": "quota_exceeded", "message": "Quota d'IP publiques atteint (2/2).",
				"field": "max_public_ips", "current": 2, "limit": 2},
		}},
	}, planCreate(true))
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), "Quota d'IP publiques atteint") {
		t.Fatalf("message de quota attendu, reçu %v", diags)
	}
	if !out.ID.IsNull() && out.ID.ValueString() != "" {
		t.Fatal("aucun VPC ne doit être enregistré après un refus")
	}
}

func TestCreate_UnprocessableRefusalPropagated(t *testing.T) {
	rapide(t)
	detail := "Aucun type d'IP de sortie n'est encore disponible (types déclarés : nat)."
	_, _, diags := runCreate(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusUnprocessableEntity, Body: map[string]any{"detail": detail}},
	}, planCreate(true))
	if !diags.HasError() || !strings.Contains(diags.Errors()[0].Detail(), detail) {
		t.Fatalf("détail 422 attendu, reçu %v", diags)
	}
}

// ── Décision d'attente ──────────────────────────────────────────────────────

func TestClassifyEgressPoll(t *testing.T) {
	st := func(s string) *client.VPC {
		v := &client.VPC{ID: vpcID}
		if s != "" {
			v.EgressIPStatus = &s
		}
		return v
	}
	cases := []struct {
		status    string
		want      bool
		wantDone  bool
		wantError bool
	}{
		{"attaching", true, false, false},
		{"attached", true, true, false},
		{"allocated", true, true, false},
		{"", true, false, false},
		{"error", true, false, true},
		{"detaching", false, false, false},
		{"attached", false, false, false},
		{"", false, true, false},
		{"error", false, false, true},
	}
	for _, c := range cases {
		done, err := classifyEgressPoll(st(c.status), c.want)
		if done != c.wantDone || (err != nil) != c.wantError {
			t.Errorf("statut %q want=%v : done=%v err=%v", c.status, c.want, done, err)
		}
	}
}

// ── Plan : egress_* inconnus seulement quand la sortie change ───────────────

func TestEgressPlanModifier(t *testing.T) {
	ctx := context.Background()
	_, _, sch := newResource(t, nil)
	prior := stateFrom(t, vpcBody(true, "attached"))
	stateRaw := toState(t, sch, prior)

	run := func(t *testing.T, dedicated bool, withState bool) types.String {
		t.Helper()
		plan := prior
		plan.DedicatedEgressIP = types.BoolValue(dedicated)
		plan.EgressIP = types.StringUnknown()
		pl := toState(t, sch, plan)
		req := planmodifier.StringRequest{
			Plan:       tfsdk.Plan{Raw: pl.Raw, Schema: sch},
			PlanValue:  types.StringUnknown(),
			State:      stateRaw,
			StateValue: prior.EgressIP,
		}
		if !withState {
			req.State = tfsdk.State{Schema: sch}
			req.StateValue = types.StringNull()
		}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		useStateUnlessEgressChanges().PlanModifyString(ctx, req, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("plan modifier : %v", resp.Diagnostics)
		}
		return resp.PlanValue
	}

	t.Run("inchangé : valeur de l'état", func(t *testing.T) {
		if got := run(t, true, true); got.ValueString() != "203.0.113.10" {
			t.Fatalf("plan = %v", got)
		}
	})
	t.Run("changé : inconnu", func(t *testing.T) {
		if got := run(t, false, true); !got.IsUnknown() {
			t.Fatalf("plan = %v, inconnu attendu", got)
		}
	})
	t.Run("création : inconnu", func(t *testing.T) {
		if got := run(t, true, false); !got.IsUnknown() {
			t.Fatalf("plan = %v, inconnu attendu", got)
		}
	})
}
