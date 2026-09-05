// Le schéma ne doit plus figer la liste des plans (#71).
package containerinstance

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ⚠️ Ce test EXÉCUTE les validateurs du schéma réel sur des clés du catalogue.
// Une assertion « il n'y a pas de `OneOf` dans le fichier » resterait verte sur
// une liste figée réintroduite autrement — et c'est précisément la forme du
// défaut : l'erreur tombait à la validation du schéma, AVANT tout appel à
// l'API, donc aucun plan ajouté au catalogue n'était atteignable sans une
// release du provider.
func TestPlan_AccepteLesFamillesDuCatalogue(t *testing.T) {
	ctx := context.Background()
	var resp resource.SchemaResponse
	New().(resource.ResourceWithConfigure).Schema(ctx, resource.SchemaRequest{}, &resp)

	attr, ok := resp.Schema.Attributes["plan"].(fwschema.StringAttribute)
	if !ok {
		t.Fatalf("l'attribut `plan` n'est plus une chaîne : %T", resp.Schema.Attributes["plan"])
	}

	// Les trois familles de `168_pricing_families_v3` : équilibrée (sans
	// préfixe), CPU (`c-`, 1 Go/vCPU) et mémoire (`m-`, 4-8 Go/vCPU).
	for _, cle := range []string{
		"small",      // historique, doit continuer de passer
		"tiny",       // équilibrée, ajoutée après la liste figée
		"small-plus", //     idem
		"xxxlarge",   //     idem
		"c-nano",     // famille CPU — le plan voulu par ocohi-proj/iaas
		"c-xxlarge",  //     idem
		"m-medium",   // famille mémoire
	} {
		for _, v := range attr.Validators {
			var vr validator.StringResponse
			v.ValidateString(ctx, validator.StringRequest{
				ConfigValue: types.StringValue(cle),
			}, &vr)
			if vr.Diagnostics.HasError() {
				t.Fatalf("le schéma refuse le plan %q avant tout appel à l'API : %v\n"+
					"le catalogue vit en base, c'est l'API qui doit trancher (elle "+
					"rend un 422 qui énumère les clés valides)", cle, vr.Diagnostics.Errors())
			}
		}
	}
}
