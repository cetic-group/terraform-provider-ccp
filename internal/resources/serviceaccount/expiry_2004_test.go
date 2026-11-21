package serviceaccount

import (
	"testing"

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
