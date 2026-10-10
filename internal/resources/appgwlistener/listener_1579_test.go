// Tests #1579 (issue #87) : certificat client sur ccp_appgw_listener, avec
// rotation en place (pas de recréation du listener).
package appgwlistener

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	gwID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	lsID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func customModel(cert, key string) listenerResourceModel {
	return listenerResourceModel{
		ID: types.StringValue(lsID), AppGWID: types.StringValue(gwID),
		Hostname: types.StringValue("shop.example.com"), AcmeChallenge: types.StringValue("custom"),
		AcmeDNSProvider: types.StringNull(), AcmeDNSCredentials: types.MapNull(types.StringType),
		AcmeStatus: types.StringValue("issued"), AcmeLastRenewalAt: types.StringNull(),
		AcmeIssuedAt: types.StringNull(), AcmeRenewAfter: types.StringNull(), AcmeLastError: types.StringNull(),
		CertPath: types.StringNull(), TLSCertificatePEM: types.StringValue(cert), TLSPrivateKeyPEM: types.StringValue(key),
		TLSCertNotAfter: types.StringNull(), TLSCertNames: types.StringNull(), TLSCertIssuer: types.StringNull(),
		CreatedAt: types.StringValue("2026-10-10T00:00:00Z"),
	}
}

func listenerBody() map[string]any {
	return map[string]any{
		"id": lsID, "appgw_id": gwID, "hostname": "shop.example.com", "acme_status": "issued",
		"acme_challenge": "custom", "tls_cert_not_after": "2027-01-01T00:00:00Z",
		"tls_cert_names": "shop.example.com", "tls_cert_issuer": "Example CA", "created_at": "2026-10-10T00:00:00Z",
	}
}

// Le certificat et la clé ne doivent porter AUCUN modificateur de
// remplacement : leur changement passe par Update (rotation en place).
func TestCertificateAttributesDoNotForceReplacement(t *testing.T) {
	var sch resource.SchemaResponse
	(&listenerResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sch)
	for _, name := range []string{"tls_certificate_pem", "tls_private_key_pem"} {
		a, ok := sch.Schema.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s absent du schéma", name)
		}
		if !a.Sensitive {
			t.Errorf("%s doit être Sensitive", name)
		}
		if len(a.PlanModifiers) != 0 {
			t.Errorf("%s ne doit pas forcer de remplacement (rotation en place)", name)
		}
	}
}

func TestUpdate_CertificateChangeRotatesInPlace(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/app-gateways/" + gwID + "/listeners/" + lsID + "/renew-cert",
			Status: http.StatusOK, Body: listenerBody()},
	})
	defer srv.Close()
	ctx := context.Background()
	r := &listenerResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)

	state, plan := customModel("OLD-CERT", "OLD-KEY"), customModel("NEW-CERT", "NEW-KEY")
	st, pl := tfsdk.State{Schema: sch.Schema}, tfsdk.State{Schema: sch.Schema}
	st.Set(ctx, &state)
	pl.Set(ctx, &plan)
	resp := &resource.UpdateResponse{State: tfsdk.State{Raw: st.Raw, Schema: sch.Schema}}
	r.Update(ctx, resource.UpdateRequest{
		Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sch.Schema}, State: tfsdk.State{Raw: st.Raw, Schema: sch.Schema},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}

	calls := srv.Calls()
	if len(calls) != 1 || !strings.HasSuffix(calls[0].Path, "/renew-cert") {
		t.Fatalf("un seul appel attendu, sur la route de renouvellement : %+v", calls)
	}
	var body map[string]string
	if err := json.Unmarshal(calls[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["tls_certificate_pem"] != "NEW-CERT" || body["tls_private_key_pem"] != "NEW-KEY" {
		t.Fatalf("corps inattendu : %v", body)
	}
	var out listenerResourceModel
	resp.State.Get(ctx, &out)
	if out.TLSPrivateKeyPEM.ValueString() != "NEW-KEY" || out.TLSCertNotAfter.ValueString() != "2027-01-01T00:00:00Z" {
		t.Fatalf("état inattendu : key=%v not_after=%v", out.TLSPrivateKeyPEM, out.TLSCertNotAfter)
	}
}

func TestValidateCustomCert(t *testing.T) {
	if d := validateCustomCert(customModel("C", "K")); d.HasError() {
		t.Fatalf("complet : %v", d)
	}
	noKey := customModel("C", "")
	noKey.TLSPrivateKeyPEM = types.StringNull()
	if !validateCustomCert(noKey).HasError() {
		t.Error("custom sans clé : erreur attendue")
	}
	le := customModel("C", "K")
	le.AcmeChallenge = types.StringValue("http01")
	if !validateCustomCert(le).HasError() {
		t.Error("certificat sans custom : erreur attendue")
	}
	unknown := customModel("C", "K")
	unknown.TLSCertificatePEM = types.StringUnknown()
	if validateCustomCert(unknown).HasError() {
		t.Error("valeur inconnue : aucune erreur avant le plan")
	}
}
