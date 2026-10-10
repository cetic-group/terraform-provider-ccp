// Tests #1579 (issue #87) : page d'indisponibilité, certificat client avec
// rotation en place, cibles scale set dynamiques, champs réseau.
//
// Les tests d'Update pilotent le VRAI Update du framework contre un serveur
// simulé et vérifient ce qui est ENVOYÉ : c'est là que se cachent les deux
// pannes que l'issue décrit (une clé absente au lieu de `null`, un listener
// recréé au lieu d'un certificat remplacé).
package loadbalancer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	lbID       = "11111111-1111-1111-1111-111111111111"
	listenerID = "22222222-2222-2222-2222-222222222222"
)

func customListener(cert, key string) lbListenerModel {
	return lbListenerModel{
		ID:                 types.StringValue(listenerID),
		Protocol:           types.StringValue("https"),
		ListenPort:         types.Int64Value(443),
		Algorithm:          types.StringValue("roundrobin"),
		HealthCheckEnabled: types.BoolValue(true),
		HealthCheckPath:    types.StringNull(),
		Domain:             types.StringValue("shop.example.com"),
		AcmeChallenge:      types.StringValue("custom"),
		AcmeDNSProvider:    types.StringNull(),
		AcmeDNSCredentials: types.MapNull(types.StringType),
		AcmeStatus:         types.StringNull(),
		AcmeLastError:      types.StringNull(),
		TLSCertificatePEM:  types.StringValue(cert),
		TLSPrivateKeyPEM:   types.StringValue(key),
		TLSCertNotAfter:    types.StringNull(),
		TLSCertNames:       types.StringNull(),
		TLSCertIssuer:      types.StringNull(),
	}
}

func baseModel(page types.String, listeners ...lbListenerModel) lbResourceModel {
	return lbResourceModel{
		ID:              types.StringValue(lbID),
		Name:            types.StringValue("web"),
		Region:          types.StringValue("RNN"),
		Plan:            types.StringValue("small"),
		VnetID:          types.StringValue("vnet-1"),
		VpcID:           types.StringNull(),
		VpcName:         types.StringNull(),
		VnetName:        types.StringNull(),
		VnetCIDR:        types.StringNull(),
		CustomErrorPage: page,
		PublicIPID:      types.StringNull(),
		VIPAddress:      types.StringNull(),
		PublicIPAddress: types.StringNull(),
		Status:          types.StringValue("active"),
		Tags:            types.ListNull(types.StringType),
		Listeners:       listeners,
		CreatedAt:       types.StringValue("2026-10-10T00:00:00Z"),
	}
}

func apiLB(page *string) map[string]any {
	return map[string]any{
		"id": lbID, "name": "web", "region": "RNN", "plan": "small", "vnet_id": "vnet-1",
		"vpc_id": "vpc-1", "vpc_name": "prod", "vnet_name": "front", "vnet_cidr": "10.0.1.0/24",
		"status": "active", "tags": []string{}, "created_at": "2026-10-10T00:00:00Z",
		"custom_error_page_html": page,
		"listeners": []map[string]any{{
			"id": listenerID, "protocol": "https", "listen_port": 443, "algorithm": "roundrobin",
			"health_check_enabled": true, "domain": "shop.example.com", "acme_challenge": "custom",
			"tls_cert_not_after": "2027-01-01T00:00:00Z", "tls_cert_names": "shop.example.com",
			"tls_cert_issuer": "Example CA", "backends": []any{},
		}},
	}
}

// runUpdate exécute le vrai Update avec un état et un plan bâtis depuis le modèle.
func runUpdate(t *testing.T, srv *testutil.Server, state, plan lbResourceModel) (lbResourceModel, *resource.UpdateResponse) {
	t.Helper()
	ctx := context.Background()
	r := &lbResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)

	st := tfsdk.State{Schema: sch.Schema}
	if d := st.Set(ctx, &state); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	pl := tfsdk.State{Schema: sch.Schema}
	if d := pl.Set(ctx, &plan); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Raw: st.Raw, Schema: sch.Schema}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: pl.Raw, Schema: sch.Schema},
		State: tfsdk.State{Raw: st.Raw, Schema: sch.Schema},
	}, resp)
	var out lbResourceModel
	if !resp.Diagnostics.HasError() {
		resp.State.Get(ctx, &out)
	}
	return out, resp
}

// ── Page d'indisponibilité ─────────────────────────────────────────────────

func TestUpdate_RemovingTheErrorPageSendsNullAndConverges(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: apiLB(nil)},
		{Method: "GET", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: apiLB(nil)},
	})
	defer srv.Close()

	l := customListener("CERT", "KEY")
	out, resp := runUpdate(t, srv,
		baseModel(types.StringValue("<h1>maintenance</h1>"), l),
		baseModel(types.StringNull(), l))
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	var patch []byte
	for _, c := range srv.Calls() {
		if c.Method == "PATCH" {
			patch = c.Body
		}
	}
	if string(patch) != `{"custom_error_page_html":null}` {
		t.Fatalf("retrait : PATCH attendu {\"custom_error_page_html\":null}, envoyé %s", patch)
	}
	if !out.CustomErrorPage.IsNull() {
		t.Fatalf("état après retrait : null attendu (plan vide ensuite), got %v", out.CustomErrorPage)
	}
}

func TestUpdate_HeredocPageDoesNotDrift(t *testing.T) {
	stored := "<h1>maintenance</h1>"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: apiLB(&stored)},
		{Method: "GET", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: apiLB(&stored)},
	})
	defer srv.Close()
	l := customListener("CERT", "KEY")
	page := types.StringValue(stored + "\n") // heredoc : saut de ligne final
	out, resp := runUpdate(t, srv, baseModel(types.StringNull(), l), baseModel(page, l))
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	if !out.CustomErrorPage.Equal(page) {
		t.Fatalf("la valeur configurée doit rester dans l'état, got %q", out.CustomErrorPage.ValueString())
	}
}

// ── Certificat client : rotation en place ──────────────────────────────────

func TestUpdate_CertificateChangeRotatesInPlace(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/load-balancers/" + lbID + "/listeners/" + listenerID + "/acme/retry", Status: 200, Body: apiLB(nil)},
		{Method: "GET", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: apiLB(nil)},
	})
	defer srv.Close()

	state := baseModel(types.StringNull(), customListener("OLD-CERT", "OLD-KEY"))
	plan := baseModel(types.StringNull(), customListener("NEW-CERT", "NEW-KEY"))

	// 1. Le plan ne force PAS le remplacement du load balancer.
	if listenersRequireReplace(plan.Listeners, state.Listeners) {
		t.Fatal("un changement de certificat ne doit pas forcer la recréation")
	}

	// 2. L'Update envoie le nouveau certificat sur la route de rotation, et rien d'autre.
	out, resp := runUpdate(t, srv, state, plan)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	var rotated map[string]string
	for _, c := range srv.Calls() {
		switch {
		case strings.HasSuffix(c.Path, "/acme/retry"):
			if err := json.Unmarshal(c.Body, &rotated); err != nil {
				t.Fatal(err)
			}
		case c.Method == "DELETE" || c.Method == "PATCH" || (c.Method == "POST" && c.Path == "/v1/load-balancers"):
			t.Fatalf("appel inattendu pendant une rotation : %s %s", c.Method, c.Path)
		}
	}
	if rotated["tls_certificate_pem"] != "NEW-CERT" || rotated["tls_private_key_pem"] != "NEW-KEY" {
		t.Fatalf("corps de rotation inattendu : %v", rotated)
	}
	// 3. La clé n'est jamais relue : l'état garde la valeur fournie.
	got := out.Listeners[0]
	if got.TLSPrivateKeyPEM.ValueString() != "NEW-KEY" || got.TLSCertificatePEM.ValueString() != "NEW-CERT" {
		t.Fatalf("certificat/clé du plan attendus dans l'état, got %v / %v", got.TLSCertificatePEM, got.TLSPrivateKeyPEM)
	}
	if got.TLSCertNotAfter.ValueString() != "2027-01-01T00:00:00Z" || got.TLSCertIssuer.ValueString() != "Example CA" {
		t.Fatalf("métadonnées du certificat non relues : %+v", got)
	}
	if out.VnetCIDR.ValueString() != "10.0.1.0/24" || out.VpcName.ValueString() != "prod" {
		t.Fatalf("champs réseau non relus : %v / %v", out.VpcName, out.VnetCIDR)
	}
}

func TestCertRotationNeeded(t *testing.T) {
	same := customListener("C", "K")
	if certRotationNeeded(same, same) {
		t.Error("certificat inchangé : pas de rotation")
	}
	if !certRotationNeeded(customListener("C", "K2"), same) {
		t.Error("clé changée : rotation attendue")
	}
	le := customListener("C2", "K")
	le.AcmeChallenge = types.StringValue("http01")
	if certRotationNeeded(le, same) {
		t.Error("listener Let's Encrypt : jamais de rotation client")
	}
}

// ── Validation ─────────────────────────────────────────────────────────────

func TestValidateListeners_CustomCertificate(t *testing.T) {
	ok := customListener("C", "K")
	if d := validateListeners([]lbListenerModel{ok}); d.HasError() {
		t.Fatalf("listener custom complet : %v", d)
	}
	noKey := customListener("C", "")
	noKey.TLSPrivateKeyPEM = types.StringNull()
	if !validateListeners([]lbListenerModel{noKey}).HasError() {
		t.Error("custom sans clé : erreur attendue")
	}
	tcp := customListener("C", "K")
	tcp.Protocol = types.StringValue("tcp")
	if !validateListeners([]lbListenerModel{tcp}).HasError() {
		t.Error("custom sur tcp : erreur attendue")
	}
	pemWithoutCustom := customListener("C", "K")
	pemWithoutCustom.AcmeChallenge = types.StringValue("http01")
	if !validateListeners([]lbListenerModel{pemWithoutCustom}).HasError() {
		t.Error("certificat sans custom : erreur attendue")
	}
	unknown := customListener("C", "K")
	unknown.TLSCertificatePEM = types.StringUnknown()
	unknown.TLSPrivateKeyPEM = types.StringNull()
	if validateListeners([]lbListenerModel{unknown}).HasError() {
		t.Error("valeur inconnue : aucune erreur avant le plan")
	}
}

func backend(ct, vm, ctss, vmss types.String) lbBackendModel {
	return lbBackendModel{ContainerID: ct, VMID: vm, ContainerScaleSetID: ctss, VMScaleSetID: vmss,
		Port: types.Int64Value(80), Weight: types.Int64Value(1)}
}

func TestValidateListeners_ExactlyOneBackendTarget(t *testing.T) {
	n, v := types.StringNull(), types.StringValue("x")
	l := customListener("C", "K")
	for name, tc := range map[string]struct {
		b  lbBackendModel
		ok bool
	}{
		"aucune":       {backend(n, n, n, n), false},
		"container":    {backend(v, n, n, n), true},
		"scale set ct": {backend(n, n, v, n), true},
		"scale set vm": {backend(n, n, n, v), true},
		"deux cibles":  {backend(v, n, v, n), false},
		"inconnue":     {backend(types.StringUnknown(), n, n, n), true},
	} {
		l.Backends = []lbBackendModel{tc.b}
		if got := !validateListeners([]lbListenerModel{l}).HasError(); got != tc.ok {
			t.Errorf("%s : valide attendu=%v", name, tc.ok)
		}
	}
}

// ── Cibles scale set ───────────────────────────────────────────────────────

func TestBackendCreateReq_ScaleSetIsOneTarget(t *testing.T) {
	n := types.StringNull()
	req := backendCreateReq(backend(n, n, types.StringValue("ss-1"), n))
	b, _ := json.Marshal(req)
	if req.ContainerScaleSetID == nil || *req.ContainerScaleSetID != "ss-1" || req.ContainerID != nil || req.VMID != nil {
		t.Fatalf("une seule cible scale set attendue, envoyé %s", b)
	}
	if backendKey(backend(n, n, types.StringValue("ss-1"), n)) == backendKey(backend(n, n, n, types.StringValue("ss-1"))) {
		t.Fatal("un scale set container et un scale set VM ne doivent pas se confondre")
	}
}

func TestStateFromAPI_MapsScaleSetBackend(t *testing.T) {
	ss := "ss-9"
	lb := &client.LoadBalancer{ID: lbID, Listeners: []client.LBListener{{
		ID: listenerID, Protocol: "http", ListenPort: 80, Algorithm: "roundrobin",
		Backends: []client.LBBackend{{ID: "be", VMScaleSetID: &ss, Port: 80, Weight: 1}},
	}}}
	m, _ := stateFromAPI(context.Background(), lb, nil, types.StringNull())
	b := m.Listeners[0].Backends[0]
	if b.VMScaleSetID.ValueString() != "ss-9" || !b.ContainerID.IsNull() || !b.VMID.IsNull() {
		t.Fatalf("backend scale set mal relu : %+v", b)
	}
}
