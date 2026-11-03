package vnet

import (
	"strings"
	"testing"
)

// cetic-cloud-platform#1734 : un 409 sur `snat = true` (pool d'IP publiques
// vide pour la sortie du VPC) ne doit pas renvoyer vers le détachement des IP
// publiques, qui ne concerne que `snat = false`.
func TestVnetConflictHint(t *testing.T) {
	vrai, faux := true, false
	if h := vnetConflictHint(nil); h != "" {
		t.Errorf("snat absent de la requête : aucune indication attendue, reçu %q", h)
	}
	if h := vnetConflictHint(&vrai); !strings.Contains(h, "egress") || strings.Contains(h, "detach") ||
		strings.Contains(h, "remain attached") {
		t.Errorf("snat = true : indication sur l'IP de sortie attendue, reçu %q", h)
	}
	if h := vnetConflictHint(&faux); !strings.Contains(h, "remain attached") || strings.Contains(h, "egress") {
		t.Errorf("snat = false : indication sur les IP attachées attendue, reçu %q", h)
	}
}
