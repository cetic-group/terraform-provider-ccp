package registry

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// cetic-cloud-platform#2004 : le PATCH de l'API n'accepte pas `name` ; sans
// RequiresReplace, un changement était annoncé puis ignoré.
func TestNameForceLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	// `image_tag` : appliqué à la création seulement, sans remplacement (une
	// montée par le back-office aurait détruit le registre, revue de #121).
	for _, nom := range []string{"name"} {
		attr, ok := resp.Schema.Attributes[nom].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s : attribut chaîne attendu", nom)
		}
		trouve := false
		for _, m := range attr.PlanModifiers {
			if strings.Contains(strings.ToLower(m.Description(context.Background())), "recreate") {
				trouve = true
			}
		}
		if !trouve {
			t.Errorf("%s doit forcer le remplacement", nom)
		}
	}
}

// `image_tag` : `CreateOnlyString` présent et AUCUN modificateur
// `RequiresReplace*` (par type, pas par description : un `RequiresReplaceIf`
// décrit autrement passerait). Une montée par le back-office aurait détruit le
// registre et ses images (revue de #121).
func TestImageTagNeForcePasLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	createOnly := false
	for _, m := range resp.Schema.Attributes["image_tag"].(schema.StringAttribute).PlanModifiers {
		typ := fmt.Sprintf("%T", m)
		if strings.Contains(strings.ToLower(typ), "requiresreplace") {
			t.Fatalf("image_tag ne doit pas forcer le remplacement (%s)", typ)
		}
		if typ == "planmodifier.createOnlyString" {
			createOnly = true
		}
	}
	if !createOnly {
		t.Fatal("image_tag doit porter CreateOnlyString")
	}
}
