// La création d'une clé « tenant-wide » n'existe plus (#78).
package objectstoragekey

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// ⚠️ Depuis IAM S3 v2 (2026-05-09), `POST /v1/object-storage/keys` rend
// **410 Gone**. Laisser l'appel partir donnait un message d'API brut au milieu
// d'un apply — vrai, mais sans dire quoi faire. On refuse au plus tôt, en
// nommant le remplaçant.
//
// Le test n'utilise AUCUN serveur : si la ressource appelait encore l'API, le
// client sans point de terminaison échouerait autrement, et le message attendu
// ne serait pas là.
func TestCreate_RefuseEtNommeLeRemplacant(t *testing.T) {
	ctx := context.Background()
	r := &keyResource{}
	var sresp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sresp)

	objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	champs := map[string]tftypes.Value{}
	for nom, typ := range objType.AttributeTypes {
		champs[nom] = tftypes.NewValue(typ, nil)
	}
	champs["label"] = tftypes.NewValue(tftypes.String, "backup-writer")
	champs["region"] = tftypes.NewValue(tftypes.String, "RNN")

	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sresp.Schema}}
	r.Create(ctx, resource.CreateRequest{
		Plan: tfsdk.Plan{Raw: tftypes.NewValue(objType, champs), Schema: sresp.Schema},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("la création est acceptée alors que l'endpoint rend 410 Gone")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	for _, attendu := range []string{"ccp_bucket_key", "410", "bucket_id"} {
		if !strings.Contains(detail, attendu) {
			t.Fatalf("le refus ne mentionne pas %q — l'utilisateur ne sait pas "+
				"quoi faire :\n%s", attendu, detail)
		}
	}
}
