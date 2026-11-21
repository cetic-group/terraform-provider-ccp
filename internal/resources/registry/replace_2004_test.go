package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// cetic-cloud-platform#2004 : le PATCH de l'API n'accepte ni `name` ni
// `image_tag` ; sans RequiresReplace, un changement était annoncé puis ignoré.
func TestNameEtImageTagForcentLeRemplacement(t *testing.T) {
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

func TestImageTagNeForcePasLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	for _, m := range resp.Schema.Attributes["image_tag"].(schema.StringAttribute).PlanModifiers {
		if strings.Contains(strings.ToLower(m.Description(context.Background())), "recreate") {
			t.Fatal("image_tag ne doit pas forcer le remplacement")
		}
	}
}
