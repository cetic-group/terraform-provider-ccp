package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBastionAccessMarshalsOnCreateRequests vérifie le contrat de fil pour
// l'opt-in « accès via le Bastion SSH » (#343/#307) : le champ `bastion_access`
// doit être présent (=true) quand activé sur les 4 ressources compute, et absent
// quand laissé à false (omitempty → défaut backend False). Le nom du champ JSON
// doit matcher exactement le schéma Pydantic backend (`bastion_access`).
func TestBastionAccessMarshalsOnCreateRequests(t *testing.T) {
	cases := []struct {
		name string
		req  any
	}{
		{"vm_instance", VMInstanceCreateRequest{Name: "x", Region: "RNN", Plan: "small", BastionAccess: true}},
		{"container", ContainerCreateRequest{Name: "x", Region: "RNN", Plan: "nano", BastionAccess: true}},
		{"vm_scale_set", VMScaleSetCreateRequest{Name: "x", Region: "RNN", Plan: "nano", BastionAccess: true}},
		{"container_scale_set", ContainerScaleSetCreateRequest{Name: "x", Region: "RNN", Plan: "nano", BastionAccess: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/true", func(t *testing.T) {
			b, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(b), `"bastion_access":true`) {
				t.Fatalf("expected bastion_access=true in payload, got: %s", b)
			}
		})
	}
}

// TestBastionAccessOmittedWhenFalse : false (défaut) ne doit PAS apparaître sur
// le fil — omitempty laisse le backend appliquer son défaut.
func TestBastionAccessOmittedWhenFalse(t *testing.T) {
	reqs := []any{
		VMInstanceCreateRequest{Name: "x", Region: "RNN", Plan: "small"},
		ContainerCreateRequest{Name: "x", Region: "RNN", Plan: "nano"},
		VMScaleSetCreateRequest{Name: "x", Region: "RNN", Plan: "nano"},
		ContainerScaleSetCreateRequest{Name: "x", Region: "RNN", Plan: "nano"},
	}
	for _, r := range reqs {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(b), "bastion_access") {
			t.Fatalf("bastion_access should be omitted when false, got: %s", b)
		}
	}
}
