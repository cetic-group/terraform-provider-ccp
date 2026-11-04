package vpc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// cetic-cloud-platform#1734 (décision du 03/11) : `dedicated_egress_ip` est
// une option du VPC, `false` par défaut (sortie partagée). `true` : IP
// publique dédiée attribuée et gardée tant que l'option l'est ; `false` : IP
// rendue au pool aussitôt. Modifiable en place (PATCH), jamais par
// remplacement. Les attributs egress_* restent en lecture seule.

func vpcSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&vpcResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestDedicatedEgressIPIsAnInPlaceOptionDefaultingToFalse(t *testing.T) {
	attr, ok := vpcSchema(t).Attributes["dedicated_egress_ip"].(schema.BoolAttribute)
	if !ok {
		t.Fatal("dedicated_egress_ip : attribut booléen absent")
	}
	if !attr.Optional || !attr.Computed || attr.Required {
		t.Error("dedicated_egress_ip : Optional+Computed attendu")
	}
	if attr.Default == nil {
		t.Fatal("dedicated_egress_ip : défaut false attendu (retirer l'attribut doit désactiver l'option)")
	}
	if len(attr.PlanModifiers) != 0 {
		t.Error("dedicated_egress_ip : aucun plan modifier attendu (pas de RequiresReplace, modifié en place)")
	}
}

func TestEgressAttributesAreReadOnlyWithConditionalModifier(t *testing.T) {
	s := vpcSchema(t)
	for _, name := range []string{"egress_ip", "egress_ip_type", "egress_ip_status"} {
		attr, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s: attribut absent ou de mauvais type", name)
		}
		if !attr.Computed || attr.Optional || attr.Required {
			t.Errorf("%s: doit être Computed seul (lecture seule)", name)
		}
		if len(attr.PlanModifiers) != 1 {
			t.Fatalf("%s: un seul plan modifier attendu (useStateUnlessEgressChanges)", name)
		}
		if _, ok := attr.PlanModifiers[0].(egressStateModifier); !ok {
			t.Errorf("%s: plan modifier conditionnel attendu, reçu %T (UseStateForUnknown figerait l'IP)", name, attr.PlanModifiers[0])
		}
	}
}

func TestNameAndTagsStillForceReplacement(t *testing.T) {
	s := vpcSchema(t)
	if len(s.Attributes["name"].(schema.StringAttribute).PlanModifiers) == 0 {
		t.Error("name : RequiresReplace attendu (non géré en place par le provider)")
	}
	if len(s.Attributes["tags"].(schema.ListAttribute).PlanModifiers) == 0 {
		t.Error("tags : RequiresReplace attendu (non géré en place par le provider)")
	}
}

func TestApplyVPCToModelEgress(t *testing.T) {
	ip, typ, st := "203.0.113.5", "edge", "attached"
	src := &client.VPC{ID: "v", Name: "n", Region: "RNN", Status: "active", CreatedAt: time.Now(),
		DedicatedEgressIP: true, EgressIP: &ip, EgressIPType: &typ, EgressIPStatus: &st}
	var m vpcResourceModel
	if d := applyVPCToModel(context.Background(), src, &m); d.HasError() {
		t.Fatalf("apply: %v", d)
	}
	if !m.DedicatedEgressIP.ValueBool() {
		t.Error("dedicated_egress_ip non recopié")
	}
	if m.EgressIP.ValueString() != ip || m.EgressIPType.ValueString() != typ ||
		m.EgressIPStatus.ValueString() != st {
		t.Fatalf("valeurs mal recopiées: %v %v %v", m.EgressIP, m.EgressIPType, m.EgressIPStatus)
	}
	// Option désactivée : l'IP est rendue, les trois passent à null.
	src.DedicatedEgressIP = false
	src.EgressIP, src.EgressIPType, src.EgressIPStatus = nil, nil, nil
	if d := applyVPCToModel(context.Background(), src, &m); d.HasError() {
		t.Fatalf("apply: %v", d)
	}
	if m.DedicatedEgressIP.IsNull() || m.DedicatedEgressIP.ValueBool() {
		t.Error("dedicated_egress_ip : false (non null) attendu")
	}
	if !m.EgressIP.IsNull() || !m.EgressIPType.IsNull() || !m.EgressIPStatus.IsNull() {
		t.Fatal("sans IP de sortie, les trois attributs doivent être null")
	}
}

func TestBuildVPCUpdateSendsOnlyTheChangedOption(t *testing.T) {
	model := func(v bool) vpcResourceModel {
		return vpcResourceModel{Name: types.StringValue("n"), DedicatedEgressIP: types.BoolValue(v)}
	}
	if _, changed := buildVPCUpdate(model(true), model(true)); changed {
		t.Error("option inchangée : aucun PATCH attendu")
	}
	for _, want := range []bool{true, false} {
		req, changed := buildVPCUpdate(model(want), model(!want))
		if !changed || req.DedicatedEgressIP == nil || *req.DedicatedEgressIP != want {
			t.Fatalf("option %t : PATCH attendu avec dedicated_egress_ip=%t, reçu %+v", want, want, req)
		}
		raw, _ := json.Marshal(req)
		want := map[bool]string{true: `{"dedicated_egress_ip":true}`, false: `{"dedicated_egress_ip":false}`}[want]
		if string(raw) != want {
			t.Errorf("corps PATCH %s, attendu %s (false doit être envoyé, pas omis)", raw, want)
		}
	}
	if raw, _ := json.Marshal(client.VPCUpdateRequest{}); string(raw) != `{}` {
		t.Errorf("champ absent = inchangé : corps vide attendu, reçu %s", raw)
	}
}

func TestCreateRequestCarriesTheOption(t *testing.T) {
	for _, v := range []bool{true, false} {
		raw, _ := json.Marshal(client.VPCCreateRequest{Name: "n", Region: "RNN", DedicatedEgressIP: v})
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if got, ok := m["dedicated_egress_ip"]; !ok || got != v {
			t.Errorf("POST /v1/vpcs : dedicated_egress_ip=%t attendu, corps %s", v, raw)
		}
	}
}

func TestEgressRefusalHint(t *testing.T) {
	c409 := &client.APIError{StatusCode: http.StatusConflict, Detail: "Aucune IP publique"}
	c422 := &client.APIError{StatusCode: http.StatusUnprocessableEntity, Detail: "non proposée"}
	if h := egressRefusalHint(c409, true); !strings.Contains(h, "no public IP is free") ||
		!strings.Contains(h, "nothing was changed") {
		t.Errorf("409 + true : indication pool vide attendue, reçu %q", h)
	}
	if h := egressRefusalHint(c422, true); !strings.Contains(h, "not offered") || !strings.Contains(h, "network architecture") {
		t.Errorf("422 + true : indication option non proposée / VPC trop ancien attendue, reçu %q", h)
	}
	if h := egressRefusalHint(c409, false); h != "" {
		t.Errorf("409 sans l'option : aucune indication d'IP attendue, reçu %q", h)
	}
	if h := egressRefusalHint(&client.APIError{StatusCode: 500}, true); h != "" {
		t.Errorf("500 : aucune indication attendue, reçu %q", h)
	}
}

// Le contrat JSON de la réponse est décodé depuis une vraie réponse HTTP
// (POST, PATCH) : un tag JSON faux ferait échouer ce test.
func TestVPCResponseDecodedFromCreateAndPatch(t *testing.T) {
	body := map[string]any{
		"id": "vpc-1", "name": "n", "region": "RNN", "cidr": "10.1.0.0/16", "status": "active",
		"tags": []string{}, "created_at": "2026-11-03T10:00:00Z", "dedicated_egress_ip": true,
		"egress_ip": "203.0.113.9", "egress_ip_type": "edge", "egress_ip_status": "allocated",
	}
	off := map[string]any{}
	for k, v := range body {
		off[k] = v
	}
	off["dedicated_egress_ip"], off["egress_ip"], off["egress_ip_type"], off["egress_ip_status"] = false, nil, nil, nil
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/vpcs", Status: http.StatusCreated, Body: body},
		{Method: "PATCH", Path: "/v1/vpcs/vpc-1", Status: http.StatusOK, Body: off},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.CreateVPC(context.Background(), client.VPCCreateRequest{Name: "n", Region: "RNN", DedicatedEgressIP: true})
	if err != nil {
		t.Fatalf("CreateVPC: %v", err)
	}
	if !got.DedicatedEgressIP || got.EgressIP == nil || *got.EgressIP != "203.0.113.9" ||
		got.EgressIPStatus == nil || *got.EgressIPStatus != "allocated" {
		t.Errorf("POST : réponse mal décodée %+v", got)
	}
	f := false
	got, err = c.UpdateVPC(context.Background(), "vpc-1", client.VPCUpdateRequest{DedicatedEgressIP: &f})
	if err != nil {
		t.Fatalf("UpdateVPC: %v", err)
	}
	if got.DedicatedEgressIP || got.EgressIP != nil || got.EgressIPStatus != nil {
		t.Errorf("PATCH false : option fausse et IP null attendues, reçu %+v", got)
	}
	calls := srv.Calls()
	if len(calls) != 2 || calls[1].Method != http.MethodPatch || string(calls[1].Body) != `{"dedicated_egress_ip":false}` {
		t.Errorf("PATCH : corps %q, attendu {\"dedicated_egress_ip\":false}", calls[1].Body)
	}
}
