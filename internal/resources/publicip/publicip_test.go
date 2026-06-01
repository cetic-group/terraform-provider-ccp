package publicip

import (
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

// Régression : le poll d'attache traitait `attaching` comme un état inattendu
// (échec dur) alors que c'est un état transitoire légitime du backend → le
// `terraform apply` échouait avec « entered unexpected state "attaching" »
// même quand l'attache aboutissait. Cf. incident 2026-05-31.

func TestClassifyAttachPoll(t *testing.T) {
	cases := []struct {
		status   string
		wantDone bool
		wantErr  bool
	}{
		{client.PublicIPStatusAttached, true, false},
		{client.PublicIPStatusAttaching, false, false}, // transitoire — keep polling
		{client.PublicIPStatusAllocated, false, false}, // task pas encore prise
		{client.PublicIPStatusError, false, true},      // échec dur
		{"banana", false, true},                        // inconnu → échec dur
	}
	for _, c := range cases {
		done, err := classifyAttachPoll("id-1", c.status)
		if done != c.wantDone {
			t.Errorf("attach %q: done=%v want %v", c.status, done, c.wantDone)
		}
		if (err != nil) != c.wantErr {
			t.Errorf("attach %q: err=%v wantErr=%v", c.status, err, c.wantErr)
		}
	}
}

func TestClassifyDetachPoll(t *testing.T) {
	cases := []struct {
		status   string
		wantDone bool
		wantErr  bool
	}{
		{client.PublicIPStatusAllocated, true, false},
		{client.PublicIPStatusDetaching, false, false}, // transitoire
		{client.PublicIPStatusAttached, false, false},  // task pas encore prise
		{client.PublicIPStatusError, false, true},
		{"banana", false, true},
	}
	for _, c := range cases {
		done, err := classifyDetachPoll("id-1", c.status)
		if done != c.wantDone {
			t.Errorf("detach %q: done=%v want %v", c.status, done, c.wantDone)
		}
		if (err != nil) != c.wantErr {
			t.Errorf("detach %q: err=%v wantErr=%v", c.status, err, c.wantErr)
		}
	}
}

// Les annotations client (label/description) doivent être mappées du record
// API vers le state, et null quand absentes (pas de chaîne vide fantôme).
func TestApplyPublicIPToModelLabels(t *testing.T) {
	lbl, desc := "passerelle-prod", "IP fixe de la passerelle"
	src := &client.PublicIP{
		ID: "pip-1", PoolID: "pool-1", Region: "RNN",
		IPAddress: "203.0.113.10", Status: client.PublicIPStatusAllocated,
		Label: &lbl, Description: &desc,
	}
	var dst publicIPResourceModel
	applyPublicIPToModel(src, &dst)
	if dst.Label.ValueString() != "passerelle-prod" {
		t.Errorf("label: got %q", dst.Label.ValueString())
	}
	if dst.Description.ValueString() != "IP fixe de la passerelle" {
		t.Errorf("description: got %q", dst.Description.ValueString())
	}

	// Absent → null.
	src2 := &client.PublicIP{ID: "pip-2", PoolID: "pool-1", Region: "RNN",
		IPAddress: "203.0.113.11", Status: client.PublicIPStatusAllocated}
	var dst2 publicIPResourceModel
	applyPublicIPToModel(src2, &dst2)
	if !dst2.Label.IsNull() || !dst2.Description.IsNull() {
		t.Errorf("expected null label/description, got %v / %v", dst2.Label, dst2.Description)
	}
}
