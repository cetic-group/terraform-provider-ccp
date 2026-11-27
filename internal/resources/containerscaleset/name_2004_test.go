package containerscaleset

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// cetic-cloud-platform#2004 : l'API ne renomme pas un scale set.
func TestNameForceLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	for _, m := range resp.Schema.Attributes["name"].(schema.StringAttribute).PlanModifiers {
		if strings.Contains(m.Description(context.Background()), "recreate") {
			return
		}
	}
	t.Fatal("name doit forcer le remplacement")
}

// Revue de #121 : les tags doivent être déjà normalisés (l'API les met en
// minuscules et dédoublonne), sinon l'état diverge après apply.
func TestTagsDoiventEtreNormalises(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr := resp.Schema.Attributes["tags"].(schema.ListAttribute)
	cas := map[string]bool{"prod": true, "env:prod": true, "équipe": true, "Prod": false, " prod": false,
		// Unicode : l'API normalise par `strip().lower()` sur tout l'Unicode.
		"Équipe": false, "prod\u00a0": false, "": false}
	for v, ok := range cas {
		l, _ := types.ListValueFrom(context.Background(), types.StringType, []string{v})
		r := &validator.ListResponse{}
		for _, val := range attr.Validators {
			val.ValidateList(context.Background(), validator.ListRequest{ConfigValue: l}, r)
		}
		if r.Diagnostics.HasError() == ok {
			t.Errorf("tag %q : accepté=%v attendu", v, ok)
		}
	}
	l, _ := types.ListValueFrom(context.Background(), types.StringType, []string{"a", "a"})
	r := &validator.ListResponse{}
	for _, val := range attr.Validators {
		val.ValidateList(context.Background(), validator.ListRequest{ConfigValue: l}, r)
	}
	if !r.Diagnostics.HasError() {
		t.Error("doublon accepté")
	}
}
