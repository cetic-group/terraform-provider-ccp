// Tests de `ccp_bucket_key` — les clés S3 scopées par bucket (#78).
package bucketkey

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

const (
	bucketID = "b-1"
	keyID    = "k-1"
)

func fixtureKey(extra map[string]any) map[string]any {
	m := map[string]any{
		"id": keyID, "bucket_id": bucketID, "label": "backup-writer",
		"region": "RNN", "access_level": "readwrite",
		"access_key_prefix": "CCP1234", "created_at": "2026-09-05T08:00:00Z",
		"last_used_at": nil, "expires_at": nil, "revoked_at": nil, "revealed_at": nil,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func schemaOf(t *testing.T) (context.Context, *keyResource, resource.SchemaResponse) {
	t.Helper()
	ctx := context.Background()
	r := &keyResource{}
	var sresp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sresp)
	return ctx, r, sresp
}

// valeurs construit une config/état complet depuis le schéma : tout à null,
// puis les champs fournis.
func valeurs(ctx context.Context, s resource.SchemaResponse, champs map[string]tftypes.Value) tftypes.Value {
	objType := s.Schema.Type().TerraformType(ctx).(tftypes.Object)
	m := map[string]tftypes.Value{}
	for nom, typ := range objType.AttributeTypes {
		m[nom] = tftypes.NewValue(typ, nil)
	}
	for nom, v := range champs {
		m[nom] = v
	}
	return tftypes.NewValue(objType, m)
}

func str(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

// ⚠️ LE test de #78 : la création vise le NOUVEL endpoint, celui scopé par
// bucket. L'ancien (`POST /v1/object-storage/keys`) rend 410 Gone depuis
// IAM S3 v2 — le serveur d'essai ne le sert pas, donc l'appeler ferait échouer
// ce test.
func TestCreate_ViseLEndpointScopeParBucket(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/buckets/" + bucketID + "/keys", Status: http.StatusCreated,
			Body: fixtureKey(map[string]any{
				"access_key": "CCP1234ABCD", "secret_key": "s3cr3t",
				"endpoint_url":   "https://s3-rnn.cloud.cetic-group.com",
				"s3_bucket_name": "tenant-a1b2-ocohi-backup",
			})},
	})
	defer srv.Close()

	ctx, r, sresp := schemaOf(t)
	r.client = client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	raw := valeurs(ctx, sresp, map[string]tftypes.Value{
		"bucket_id": str(bucketID), "label": str("backup-writer"),
		"access_level": str("readwrite"),
	})
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sresp.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: raw, Schema: sresp.Schema}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics.Errors())
	}

	var etat keyModel
	if d := resp.State.Get(ctx, &etat); d.HasError() {
		t.Fatalf("State.Get: %v", d.Errors())
	}
	if etat.SecretKey.ValueString() != "s3cr3t" {
		t.Fatalf("le secret n'atteint pas l'état : %q", etat.SecretKey.ValueString())
	}
	// ⚠️ `s3_bucket_name` diffère du nom affiché et c'est LUI qu'attendent les
	// outils externes. Le perdre obligerait à le deviner.
	if etat.S3BucketName.ValueString() != "tenant-a1b2-ocohi-backup" {
		t.Fatalf("s3_bucket_name perdu : %q", etat.S3BucketName.ValueString())
	}
	if etat.EndpointURL.IsNull() || etat.AccessKey.IsNull() {
		t.Fatalf("endpoint ou access_key manquants : %+v", etat)
	}
}

// ⚠️ Une clé RÉVOQUÉE répond encore 200 : la suppression est en deux temps
// côté API (le premier DELETE pose `revoked_at`, le second efface la ligne).
// Sans ce traitement, une révocation faite hors de Terraform passerait
// inaperçue et l'état annoncerait une clé qui n'ouvre plus rien.
func TestRead_UneCleRevoqueeSortDeLEtat(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/" + bucketID + "/keys/" + keyID, Status: http.StatusOK,
			Body: fixtureKey(map[string]any{"revoked_at": "2026-09-05T09:00:00Z"})},
	})
	defer srv.Close()

	ctx, r, sresp := schemaOf(t)
	r.client = client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	raw := valeurs(ctx, sresp, map[string]tftypes.Value{
		"id": str(keyID), "bucket_id": str(bucketID), "label": str("backup-writer"),
		"access_level": str("readwrite"),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sresp.Schema}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sresp.Schema}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics.Errors())
	}
	if !resp.State.Raw.IsNull() {
		t.Fatal("une clé révoquée reste dans l'état : la prochaine exécution " +
			"croira disposer d'un accès qui n'ouvre plus rien")
	}
}

// Le témoin : une clé vivante DOIT rester dans l'état — sinon « sortir de
// l'état » passerait aussi en retirant tout le monde.
func TestRead_UneCleVIVANTE_reste(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/" + bucketID + "/keys/" + keyID,
			Status: http.StatusOK, Body: fixtureKey(nil)},
	})
	defer srv.Close()

	ctx, r, sresp := schemaOf(t)
	r.client = client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	raw := valeurs(ctx, sresp, map[string]tftypes.Value{
		"id": str(keyID), "bucket_id": str(bucketID), "label": str("backup-writer"),
		"access_level": str("readwrite"), "secret_key": str("s3cr3t"),
	})
	resp := &resource.ReadResponse{State: tfsdk.State{Raw: raw, Schema: sresp.Schema}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Raw: raw, Schema: sresp.Schema}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics.Errors())
	}
	if resp.State.Raw.IsNull() {
		t.Fatal("une clé vivante a été retirée de l'état")
	}

	var etat keyModel
	if d := resp.State.Get(ctx, &etat); d.HasError() {
		t.Fatalf("State.Get: %v", d.Errors())
	}
	// ⚠️ La lecture ne doit PAS effacer le secret : l'API ne le rend jamais, et
	// l'écraser avec une valeur vide le perdrait au premier `refresh`.
	if etat.SecretKey.ValueString() != "s3cr3t" {
		t.Fatalf("la lecture a effacé le secret : %q", etat.SecretKey.ValueString())
	}
}

// L'import prend `<bucket_id>/<key_id>` : la clé est scopée sous son bucket,
// l'identifiant seul ne la retrouve pas.
func TestImport_ExigeLeCoupleBucketEtCle(t *testing.T) {
	ctx, r, sresp := schemaOf(t)

	// ⚠️ Un état NUL MAIS TYPÉ, comme le framework en fournit à l'import : un
	// `tfsdk.State` sans `Raw` refuse toute écriture d'attribut, et l'échec
	// ressemblerait à un défaut du provider.
	etatVide := func() tfsdk.State {
		objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
		return tfsdk.State{Raw: tftypes.NewValue(objType, nil), Schema: sresp.Schema}
	}

	for _, mauvais := range []string{"k-1", "", "/k-1", "b-1/"} {
		resp := &resource.ImportStateResponse{State: etatVide()}
		r.ImportState(ctx, resource.ImportStateRequest{ID: mauvais}, resp)
		if !resp.Diagnostics.HasError() {
			t.Fatalf("l'identifiant %q est accepté alors qu'il ne désigne pas une clé", mauvais)
		}
	}

	resp := &resource.ImportStateResponse{State: etatVide()}
	r.ImportState(ctx, resource.ImportStateRequest{ID: bucketID + "/" + keyID}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("un identifiant valide est refusé : %v", resp.Diagnostics.Errors())
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Fatal("l'import doit AVERTIR que `secret_key` restera nul — la " +
			"révélation est à usage unique côté API")
	}
}
