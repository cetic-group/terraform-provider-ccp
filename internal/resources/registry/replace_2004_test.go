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
	for _, nom := range []string{"name", "image_tag"} {
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
