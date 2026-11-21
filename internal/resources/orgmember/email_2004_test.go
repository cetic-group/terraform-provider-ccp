package orgmember

import (
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
