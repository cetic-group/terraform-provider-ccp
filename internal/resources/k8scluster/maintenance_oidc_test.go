package k8scluster

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var platformDefaults = client.K8sMaintenance{
	AutoPatch: true,
	Fenetre:   client.K8sMaintenanceFenetre{Jours: []int{2, 3, 4}, HeureDebut: "02:00", DureeHeures: 4, Fuseau: "Europe/Paris"},
	DatesGel:  []client.K8sMaintenanceGel{},
}

func fenetreObj(jours []int64, heure string) types.Object {
	js := []attr.Value{}
	for _, j := range jours {
		js = append(js, types.Int64Value(j))
	}
	return types.ObjectValueMust(fenetreAttrTypes, map[string]attr.Value{
		"jours":        types.ListValueMust(types.Int64Type, js),
		"heure_debut":  types.StringValue(heure),
		"duree_heures": types.Int64Value(4),
		"fuseau":       types.StringValue("Europe/Paris"),
	})
}

func maintObj(autoPatch, autoMineure attr.Value, fenetre types.Object, gels types.List) types.Object {
	return types.ObjectValueMust(maintenanceAttrTypes, map[string]attr.Value{
		"auto_patch":   autoPatch,
		"auto_mineure": autoMineure,
		"auto_majeure": types.BoolNull(),
		"fenetre":      fenetre,
		"dates_gel":    gels,
		"personnalise": types.BoolNull(),
	})
}

var gelsNull = types.ListNull(types.ObjectType{AttrTypes: gelAttrTypes})

// The PUT carries only the configured fields that differ from the state:
// an unset field keeps the platform value (console changes included), an
// unchanged one is not sent again.
func TestMaintenanceRequest_OnlyConfiguredChanges(t *testing.T) {
	ctx := context.Background()
	prior, d := maintenanceValue(ctx, &platformDefaults, types.ObjectNull(maintenanceAttrTypes))
	if d.HasError() {
		t.Fatal(d)
	}

	// Nothing configured: nothing sent.
	if _, send, _ := maintenanceRequest(ctx, types.ObjectNull(maintenanceAttrTypes), prior); send {
		t.Fatal("no maintenance configured: no PUT")
	}
	// Same values as the state, days in another order: nothing sent.
	cfg := maintObj(types.BoolValue(true), types.BoolNull(), fenetreObj([]int64{4, 2, 3}, "02:00"), gelsNull)
	if req, send, _ := maintenanceRequest(ctx, cfg, prior); send {
		t.Fatalf("unchanged settings must not be sent again: %+v", req)
	}
	// auto_mineure turned on: only that field.
	cfg = maintObj(types.BoolValue(true), types.BoolValue(true), types.ObjectNull(fenetreAttrTypes), gelsNull)
	req, send, _ := maintenanceRequest(ctx, cfg, prior)
	if !send {
		t.Fatal("auto_mineure changed: PUT expected")
	}
	raw, _ := json.Marshal(req)
	if string(raw) != `{"auto_mineure":true}` {
		t.Fatalf("PUT body = %s, want only auto_mineure", raw)
	}
	// Creation (no prior): every configured field.
	cfg = maintObj(types.BoolValue(false), types.BoolNull(), fenetreObj([]int64{6}, "03:30"),
		types.ListValueMust(types.ObjectType{AttrTypes: gelAttrTypes}, []attr.Value{}))
	req, _, _ = maintenanceRequest(ctx, cfg, types.ObjectNull(maintenanceAttrTypes))
	raw, _ = json.Marshal(req)
	want := `{"auto_patch":false,"fenetre":{"jours":[6],"heure_debut":"03:30","duree_heures":4,"fuseau":"Europe/Paris"},"dates_gel":[]}`
	if string(raw) != want {
		t.Fatalf("PUT body = %s\nwant %s", raw, want)
	}
}

// The API sorts the days; the state keeps the configured order when it is
// the same window (no "inconsistent result", no diff).
func TestMaintenanceValue_KeepsConfiguredSpelling(t *testing.T) {
	ctx := context.Background()
	planned := maintObj(types.BoolValue(true), types.BoolUnknown(), fenetreObj([]int64{4, 2, 3}, "02:00"), gelsNull)
	got, d := maintenanceValue(ctx, &platformDefaults, planned)
	if d.HasError() {
		t.Fatal(d)
	}
	var m maintenanceModel
	got.As(ctx, &m, basetypes.ObjectAsOptions{})
	if !m.Fenetre.Equal(fenetreObj([]int64{4, 2, 3}, "02:00")) {
		t.Fatalf("fenetre = %s, want the configured order", m.Fenetre)
	}
	if m.AutoMineure.IsUnknown() || m.AutoMineure.ValueBool() {
		t.Fatalf("auto_mineure = %s, want the API false", m.AutoMineure)
	}
	// A different window: the API's.
	planned = maintObj(types.BoolValue(true), types.BoolValue(false), fenetreObj([]int64{1}, "02:00"), gelsNull)
	got, _ = maintenanceValue(ctx, &platformDefaults, planned)
	got.As(ctx, &m, basetypes.ObjectAsOptions{})
	if !m.Fenetre.Equal(fenetreObj([]int64{2, 3, 4}, "02:00")) {
		t.Fatalf("fenetre = %s, want the API one", m.Fenetre)
	}
}

func oidcObj(issuer, groups string, scopes []string, ca types.String) types.Object {
	sc := []attr.Value{}
	for _, s := range scopes {
		sc = append(sc, types.StringValue(s))
	}
	return types.ObjectValueMust(oidcAttrTypes, map[string]attr.Value{
		"issuer_url":      types.StringValue(issuer),
		"client_id":       types.StringValue("kubectl"),
		"username_claim":  types.StringValue("email"),
		"username_prefix": types.StringValue("oidc:"),
		"groups_claim":    types.StringValue(groups),
		"groups_prefix":   types.StringValue("oidc:"),
		"extra_scopes":    types.ListValueMust(types.StringType, sc),
		"ca_pem":          ca,
	})
}

const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"

// What the API stores (normaliser): the state keeps the configured spelling
// when it is the same configuration, so a refresh plans nothing.
func TestOIDCValue_SameConfigurationKeepsSpelling(t *testing.T) {
	ctx := context.Background()
	g := "groups"
	ca := pem + "\n"
	api := &client.K8sClusterOIDC{
		IssuerURL: "https://sso.example.com/realms/x", ClientID: "kubectl", UsernameClaim: "email",
		UsernamePrefix: "oidc:", GroupsClaim: &g, GroupsPrefix: "oidc:", ExtraScopes: []string{"profile"}, CaPEM: &ca,
	}
	prior := oidcObj(" https://sso.example.com/realms/x ", "groups", []string{"openid", "profile", "profile"},
		types.StringValue("\n"+pem+"\n\n"))
	got, d := oidcValue(ctx, api, prior)
	if d.HasError() || !got.Equal(prior) {
		t.Fatalf("same configuration must keep the prior value, got %s", got)
	}
	// Empty groups claim = no groups (the API stores null).
	api.GroupsClaim = nil
	prior = oidcObj("https://sso.example.com/realms/x", "", []string{"profile"}, types.StringValue(pem))
	if got, _ := oidcValue(ctx, api, prior); !got.Equal(prior) {
		t.Fatalf("groups_claim \"\" ≡ null, got %s", got)
	}
	// Changed outside Terraform: the API's.
	api.IssuerURL = "https://other.example.com"
	got, _ = oidcValue(ctx, api, prior)
	var m oidcModel
	got.As(ctx, &m, basetypes.ObjectAsOptions{})
	if m.IssuerURL.ValueString() != "https://other.example.com" {
		t.Fatalf("drift not read back: %s", got)
	}
	if got, _ := oidcValue(ctx, nil, prior); !got.IsNull() {
		t.Fatal("no SSO on the API: null")
	}
}

// The PUT restarts the apiserver: never sent when the configuration is the
// same once canonical; DELETE when removed; PUT with every field otherwise.
func TestApplyOIDC_SendsOnlyOnChange(t *testing.T) {
	ctx := context.Background()
	same := oidcObj("https://sso.example.com", "groups", nil, types.StringNull())
	spelled := oidcObj("https://sso.example.com ", "groups", []string{"openid"}, types.StringValue("  "))

	// No route at all: any call fails the test (testutil reports it).
	srv := testutil.NewTestServer(t, nil)
	r := &k8sResource{client: client.New(srv.URL, "k", "test")}
	if got, d := r.applyOIDC(ctx, "cl-1", spelled, same); got != nil || d.HasError() {
		t.Fatalf("same configuration must send nothing (%v, %v)", got, d)
	}
	if got, d := r.applyOIDC(ctx, "cl-1", types.ObjectNull(oidcAttrTypes), types.ObjectNull(oidcAttrTypes)); got != nil || d.HasError() {
		t.Fatal("no SSO before nor after: nothing sent")
	}
	srv.Close()

	srv = testutil.NewTestServer(t, testutil.Routes{
		{Method: "PUT", Path: "/v1/k8s/clusters/cl-1/oidc", Status: http.StatusOK, Body: activeV2143("standard")},
		{Method: "DELETE", Path: "/v1/k8s/clusters/cl-1/oidc", Status: http.StatusOK, Body: activeV2143("standard")},
	})
	defer srv.Close()
	r = &k8sResource{client: client.New(srv.URL, "k", "test")}
	changed := oidcObj("https://sso.example.com", "", nil, types.StringNull())
	if _, d := r.applyOIDC(ctx, "cl-1", changed, same); d.HasError() {
		t.Fatal(d)
	}
	if _, d := r.applyOIDC(ctx, "cl-1", types.ObjectNull(oidcAttrTypes), same); d.HasError() {
		t.Fatal(d)
	}
	calls := srv.Calls()
	if len(calls) != 2 || calls[0].Method != "PUT" || calls[1].Method != "DELETE" {
		t.Fatalf("calls = %+v", calls)
	}
	body := string(calls[0].Body)
	for _, want := range []string{`"groups_claim":null`, `"ca_pem":null`, `"extra_scopes":[]`, `"username_claim":"email"`} {
		if !strings.Contains(body, want) {
			t.Errorf("PUT body %s lacks %s", body, want)
		}
	}
}

// Terraform core takes an Optional nested attribute whose prior value holds a
// non-Computed child for a configured one (objchange optionalValueNotComputable):
// a configuration without `maintenance` would then plan its removal at every
// run. Every attribute under an Optional+Computed nested attribute must be
// Computed.
func TestSchema_OptionalComputedNestedChildrenAreComputed(t *testing.T) {
	resp := &resource.SchemaResponse{}
	New().Schema(context.Background(), resource.SchemaRequest{}, resp)
	var walk func(path string, attrs map[string]schema.Attribute)
	walk = func(path string, attrs map[string]schema.Attribute) {
		for name, a := range attrs {
			p := path + "." + name
			if !a.IsComputed() {
				t.Errorf("%s is not Computed under an Optional+Computed nested attribute", p)
			}
			switch n := a.(type) {
			case schema.SingleNestedAttribute:
				walk(p, n.Attributes)
			case schema.ListNestedAttribute:
				walk(p, n.NestedObject.Attributes)
			}
		}
	}
	for name, a := range resp.Schema.Attributes {
		if n, ok := a.(schema.SingleNestedAttribute); ok && n.Optional && n.Computed {
			walk(name, n.Attributes)
		}
	}
}
