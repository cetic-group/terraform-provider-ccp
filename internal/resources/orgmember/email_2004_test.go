package orgmember

import (
	"context"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Revue de #121 : l'API rend l'e-mail en minuscules.
func TestSetStateGardeLaCasseDeLaConfiguration(t *testing.T) {
	m := memberResourceModel{Email: types.StringValue("Alice@Exemple.fr")}
	setState(&m, &client.OrgMember{ID: "m1", Email: "alice@exemple.fr", Role: "member", CreatedAt: time.Now()})
	if m.Email.ValueString() != "Alice@Exemple.fr" {
		t.Fatalf("casse de la configuration perdue : %v", m.Email)
	}
	setState(&m, &client.OrgMember{ID: "m1", Email: "bob@exemple.fr", Role: "member", CreatedAt: time.Now()})
	if m.Email.ValueString() != "bob@exemple.fr" {
		t.Fatalf("un autre e-mail doit être repris de l'API : %v", m.Email)
	}
}

// Revue de #121 : après `terraform import`, l'état porte l'e-mail en
// minuscules ; une config `Alice@…` ne doit pas remplacer le membre. Un autre
// e-mail, si.
func TestEmailRemplaceSeulementPourUnAutreEmail(t *testing.T) {
	ctx := context.Background()
	var sresp resource.SchemaResponse
	New().Schema(ctx, resource.SchemaRequest{}, &sresp)
	attr := sresp.Schema.Attributes["email"].(schema.StringAttribute)
	for _, c := range []struct {
		config, etat string
		remplace     bool
	}{
		{"Alice@Exemple.fr", "alice@exemple.fr", false},
		{"bob@exemple.fr", "alice@exemple.fr", true},
	} {
		state := tfsdk.State{Schema: sresp.Schema, Raw: tftypes.NewValue(sresp.Schema.Type().TerraformType(ctx), map[string]tftypes.Value{
			"id": tftypes.NewValue(tftypes.String, "m1"), "email": tftypes.NewValue(tftypes.String, c.etat),
			"role": tftypes.NewValue(tftypes.String, "member"), "accepted": tftypes.NewValue(tftypes.Bool, false),
			"accepted_at": tftypes.NewValue(tftypes.String, nil), "created_at": tftypes.NewValue(tftypes.String, "x"),
		})}
		req := planmodifier.StringRequest{
			Path: path.Root("email"), State: state, StateValue: types.StringValue(c.etat),
			PlanValue: types.StringValue(c.config), ConfigValue: types.StringValue(c.config),
			Plan: tfsdk.Plan{Schema: sresp.Schema, Raw: state.Raw},
		}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		for _, m := range attr.PlanModifiers {
			m.PlanModifyString(ctx, req, resp)
		}
		if resp.RequiresReplace != c.remplace {
			t.Errorf("%s → %s : remplacement=%v, attendu %v", c.etat, c.config, resp.RequiresReplace, c.remplace)
		}
	}
}
