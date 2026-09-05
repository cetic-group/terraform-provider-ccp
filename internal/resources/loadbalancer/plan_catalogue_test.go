// Le schéma ne doit plus figer la liste des plans (#71).
package loadbalancer

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ⚠️ Contrairement au bastion et à la passerelle VPN — dont l'API fige
// elle-même `("small","medium","large")` et refuse le reste en 422 —, celle du
// load balancer accepte `plan: str` sans validateur et le résout au catalogue
// (`compute_plans`, clé `lb-<plan>`, kind `lb`). Le `OneOf` du provider y
// rendait donc inaccessible tout plan ajouté au backoffice, sans que l'API
// n'ait rien à y redire.
//
// Le test EXÉCUTE les validateurs du schéma réel : une lecture de source
// resterait verte sur une liste figée réintroduite autrement.
func TestPlan_NeFigePasLeCatalogue(t *testing.T) {
	ctx := context.Background()
	var resp resource.SchemaResponse
	New().(resource.ResourceWithConfigure).Schema(ctx, resource.SchemaRequest{}, &resp)

	attr, ok := resp.Schema.Attributes["plan"].(fwschema.StringAttribute)
	if !ok {
		t.Fatalf("l'attribut `plan` n'est plus une chaîne : %T", resp.Schema.Attributes["plan"])
	}

	for _, cle := range []string{"small", "medium", "large", "xlarge", "edge"} {
		for _, v := range attr.Validators {
			var vr validator.StringResponse
			v.ValidateString(ctx, validator.StringRequest{
				ConfigValue: types.StringValue(cle),
			}, &vr)
			if vr.Diagnostics.HasError() {
				t.Fatalf("le schéma refuse le plan %q avant tout appel à l'API : %v\n"+
					"l'API du load balancer ne fige aucune liste — elle résout la clé "+
					"au catalogue", cle, vr.Diagnostics.Errors())
			}
		}
	}
}
