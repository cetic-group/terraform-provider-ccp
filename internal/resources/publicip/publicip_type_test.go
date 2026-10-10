package publicip

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// #1612 — `type` est lu sur l'API et projeté tel quel dans l'état.
func TestApplyPublicIPToModelType(t *testing.T) {
	fw := "firewall"
	var dst publicIPResourceModel
	applyPublicIPToModel(&client.PublicIP{ID: "pip-1", Type: &fw}, &dst)
	if dst.Type.ValueString() != "firewall" {
		t.Errorf("type: got %v", dst.Type)
	}
}

// API antérieure à #1612 : pas de `type` rendu. Une valeur encore inconnue
// (création sans `type` configuré) doit devenir null — un inconnu après apply
// ferait échouer Terraform —, et un type déjà connu ne doit pas être effacé.
func TestApplyPublicIPToModelTypeAbsent(t *testing.T) {
	unknown := publicIPResourceModel{Type: types.StringUnknown()}
	applyPublicIPToModel(&client.PublicIP{ID: "pip-1"}, &unknown)
	if !unknown.Type.IsNull() {
		t.Errorf("unknown → expected null, got %v", unknown.Type)
	}

	known := publicIPResourceModel{Type: types.StringValue("edge")}
	applyPublicIPToModel(&client.PublicIP{ID: "pip-1"}, &known)
	if known.Type.ValueString() != "edge" {
		t.Errorf("known type must be kept, got %v", known.Type)
	}
}

// Le piège de la montée de version : l'état des IP déjà allouées n'a pas de
// `type` (null) et, l'attribut n'étant pas configuré, le plan vaut « inconnu ».
// Avec RequiresReplace, chaque IP existante serait détruite puis réallouée —
// donc perdue. Seul un type CONFIGURÉ et différent doit forcer le remplacement.
func TestTypeChangeReplacesOnlyWhenConfigured(t *testing.T) {
	var sch resource.SchemaResponse
	(&publicIPResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sch)
	attr, ok := sch.Schema.Attributes["type"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("attribut `type` absent du schéma")
	}

	objType := sch.Schema.Type().TerraformType(context.Background())

	run := func(state, plan, config types.String) bool {
		req := planmodifier.StringRequest{
			StateValue:  state,
			PlanValue:   plan,
			ConfigValue: config,
			State:       tfsdk.State{Raw: nonNullObject(objType)},
			Plan:        tfsdk.Plan{Raw: nonNullObject(objType)},
		}
		resp := &planmodifier.StringResponse{PlanValue: plan}
		for _, m := range attr.PlanModifiers {
			m.PlanModifyString(context.Background(), req, resp)
			req.PlanValue = resp.PlanValue
		}
		return resp.RequiresReplace
	}

	if run(types.StringNull(), types.StringUnknown(), types.StringNull()) {
		t.Error("montée de version (état null, non configuré) : aucun remplacement attendu")
	}
	if run(types.StringValue("edge"), types.StringValue("edge"), types.StringNull()) {
		t.Error("type inchangé : aucun remplacement attendu")
	}
	if !run(types.StringValue("edge"), types.StringValue("firewall"), types.StringValue("firewall")) {
		t.Error("type configuré différent : remplacement attendu")
	}
}

// nonNullObject rend un objet non nul dont chaque attribut vaut null : il
// suffit aux modificateurs, qui ne testent que la nullité du Raw.
func nonNullObject(objType tftypes.Type) tftypes.Value {
	ot := objType.(tftypes.Object)
	vals := make(map[string]tftypes.Value, len(ot.AttributeTypes))
	for k, t := range ot.AttributeTypes {
		vals[k] = tftypes.NewValue(t, nil)
	}
	return tftypes.NewValue(objType, vals)
}
