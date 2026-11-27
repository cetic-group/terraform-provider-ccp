package serviceaccount

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// cetic-cloud-platform#2004 : l'API arrondit l'expiration en jours ; relire sa
// valeur exacte rendait l'état incohérent avec la configuration.
func TestKeepConfiguredExpiry(t *testing.T) {
	conf := types.StringValue("2027-01-15T12:00:00Z")
	cases := []struct {
		name string
		api  types.String
		want types.String
	}{
		{"même jour, heure différente", types.StringValue("2027-01-16T09:31:07Z"), conf},
		{"écart de 24 h 30 (arrondi au jour + latence)", types.StringValue("2027-01-16T12:30:00Z"), conf},
		// Revue de #121 : raccourcir l'échéance d'un jour doit produire un diff.
		{"écart de 30 h", types.StringValue("2027-01-16T18:00:00Z"), types.StringValue("2027-01-16T18:00:00Z")},
		{"écart de plus de 2 jours", types.StringValue("2027-02-15T12:00:00Z"), types.StringValue("2027-02-15T12:00:00Z")},
		{"plus d'expiration côté API", types.StringNull(), types.StringNull()},
	}
	for _, c := range cases {
		if got := keepConfiguredExpiry(conf, c.api); !got.Equal(c.want) {
			t.Errorf("%s : %v, attendu %v", c.name, got, c.want)
		}
	}
	if got := keepConfiguredExpiry(types.StringNull(), types.StringValue("2027-01-16T09:31:07Z")); got.ValueString() != "2027-01-16T09:31:07Z" {
		t.Errorf("sans valeur configurée, la valeur de l'API doit être reprise : %v", got)
	}
}

// Revue de #121 : `TimeWithinTolerance` doit passer AVANT `RequiresReplace`
// dans le schéma de `expires_at` — sinon le premier plan après la montée du
// provider remplace le compte (état écrit avec l'heure exacte de l'API).
func TestToleranceAvantRequiresReplace(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	tolerance, remplacement := -1, -1
	for i, m := range resp.Schema.Attributes["expires_at"].(schema.StringAttribute).PlanModifiers {
		switch typ := fmt.Sprintf("%T", m); {
		case typ == "planmodifier.timeWithinTolerance":
			tolerance = i
		case strings.Contains(strings.ToLower(typ), "requiresreplace"):
			remplacement = i
		}
	}
	if tolerance < 0 || remplacement < 0 || tolerance > remplacement {
		t.Fatalf("ordre attendu : tolérance (%d) avant remplacement (%d)", tolerance, remplacement)
	}
}

// Une valeur inconnue au plan (`timeadd(timestamp(), …)`, ressource `time_*`
// recréée) doit remplacer le compte dès le plan. Terraform refuse qu'une
// action annoncée « update » devienne un remplacement à l'apply, quand la
// valeur devient connue (« inconsistent final plan »).
func TestExpirationInconnueRemplaceDesLePlan(t *testing.T) {
	var sr resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &sr)
	brut := tftypes.NewValue(tftypes.String, "présent")
	req := planmodifier.StringRequest{
		Path:        path.Root("expires_at"),
		State:       tfsdk.State{Raw: brut},
		Plan:        tfsdk.Plan{Raw: brut},
		StateValue:  types.StringValue("2027-01-15T12:00:00Z"),
		ConfigValue: types.StringUnknown(),
		PlanValue:   types.StringUnknown(),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	for _, m := range sr.Schema.Attributes["expires_at"].(schema.StringAttribute).PlanModifiers {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%T", m)), "usestateforunknown") {
			continue // sans effet quand la config est inconnue
		}
		req.PlanValue = resp.PlanValue
		m.PlanModifyString(context.Background(), req, resp)
	}
	if !resp.RequiresReplace {
		t.Fatal("une échéance inconnue au plan doit remplacer le compte dès le plan")
	}
}
