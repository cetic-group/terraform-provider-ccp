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
