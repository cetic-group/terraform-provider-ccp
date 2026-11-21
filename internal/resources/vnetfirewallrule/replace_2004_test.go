package vnetfirewallrule

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// cetic-cloud-platform#2004 : pas de PATCH pour une règle côté API ; comment,
// position et enabled doivent recréer la règle au lieu d'appeler une route
// inexistante.
func TestAttributsModifiablesForcentLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	descs := map[string][]string{}
	for nom, a := range resp.Schema.Attributes {
		switch v := a.(type) {
		case schema.StringAttribute:
			for _, m := range v.PlanModifiers {
				descs[nom] = append(descs[nom], m.Description(context.Background()))
			}
		case schema.Int64Attribute:
			for _, m := range v.PlanModifiers {
				descs[nom] = append(descs[nom], m.Description(context.Background()))
			}
		case schema.BoolAttribute:
			for _, m := range v.PlanModifiers {
				descs[nom] = append(descs[nom], m.Description(context.Background()))
			}
		}
	}
	for _, nom := range []string{"comment", "enabled"} {
		if !strings.Contains(strings.Join(descs[nom], " "), "recreate") {
			t.Errorf("%s doit forcer le remplacement", nom)
		}
	}
	// `position` : fixée à la création, jamais recréée (le réordonnancement de
	// la console renumérote toutes les règles, revue de #121).
	if strings.Contains(strings.Join(descs["position"], " "), "recreate") {
		t.Error("position ne doit pas forcer le remplacement")
	}
}
