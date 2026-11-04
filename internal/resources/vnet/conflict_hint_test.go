package vnet

import (
	"strings"
	"testing"
)

// cetic-cloud-platform#1734 (décision du 03/11) : un VNet `snat = true` ne
// prend plus aucune IP publique (sortie partagée par défaut, IP dédiée en
// option du VPC). Aucun 409 « pas d'IP » ne peut donc survenir sur `true` :
// pas d'indication. `snat = false` garde la sienne (IP routées attachées).
func TestVnetConflictHint(t *testing.T) {
	vrai, faux := true, false
	if h := vnetConflictHint(nil); h != "" {
		t.Errorf("snat absent de la requête : aucune indication attendue, reçu %q", h)
	}
	if h := vnetConflictHint(&vrai); h != "" {
		t.Errorf("snat = true : aucune indication attendue (aucune IP prise), reçu %q", h)
	}
	if h := vnetConflictHint(&faux); !strings.Contains(h, "remain attached") || strings.Contains(h, "egress") {
		t.Errorf("snat = false : indication sur les IP attachées attendue, reçu %q", h)
	}
}
