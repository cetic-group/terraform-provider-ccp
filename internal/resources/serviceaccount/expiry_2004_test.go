package serviceaccount

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// Revue de #121 : `expires_at = timeadd(timestamp(), "720h")` est inconnu à
// chaque plan ; il ne doit pas remplacer un compte qui a déjà une échéance.
func TestExpirationInconnueNeRemplacePas(t *testing.T) {
	cas := []struct {
		nom          string
		plan, etat   types.String
		remplacement bool
	}{
		{"inconnue, échéance en état", types.StringUnknown(), types.StringValue("2027-01-15T12:00:00Z"), false},
		{"inconnue, sans échéance", types.StringUnknown(), types.StringNull(), true},
		{"connue et différente", types.StringValue("2027-03-01T00:00:00Z"), types.StringValue("2027-01-15T12:00:00Z"), true},
	}
	for _, c := range cas {
		resp := &stringplanmodifier.RequiresReplaceIfFuncResponse{}
		remplacerSiExpirationConnue(context.Background(),
			planmodifier.StringRequest{PlanValue: c.plan, StateValue: c.etat}, resp)
		if resp.RequiresReplace != c.remplacement {
			t.Errorf("%s : remplacement = %v, attendu %v", c.nom, resp.RequiresReplace, c.remplacement)
		}
	}
}
