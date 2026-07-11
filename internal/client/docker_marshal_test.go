package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDockerMarshalsOnContainerCreateRequests vérifie le contrat de fil pour
// l'opt-in Docker (nesting) : le champ `docker` doit être présent (=true) quand
// activé sur les 2 ressources container, et absent quand laissé à false
// (omitempty → défaut backend False = conteneur durci). Le nom du champ JSON
// doit matcher exactement le schéma Pydantic backend (`docker`).
func TestDockerMarshalsOnContainerCreateRequests(t *testing.T) {
	cases := []struct {
		name string
		req  any
	}{
		{"container", ContainerCreateRequest{Name: "x", Region: "RNN", Plan: "nano", Docker: true}},
		{"container_scale_set", ContainerScaleSetCreateRequest{Name: "x", Region: "RNN", Plan: "nano", Docker: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/true", func(t *testing.T) {
			b, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(b), `"docker":true`) {
				t.Fatalf("expected docker=true in payload, got: %s", b)
			}
		})
	}
}

// TestDockerOmittedWhenFalse : false (défaut) ne doit PAS apparaître sur le fil.
func TestDockerOmittedWhenFalse(t *testing.T) {
	reqs := []any{
		ContainerCreateRequest{Name: "x", Region: "RNN", Plan: "nano"},
		ContainerScaleSetCreateRequest{Name: "x", Region: "RNN", Plan: "nano"},
	}
	for _, r := range reqs {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(b), "docker") {
			t.Fatalf("docker should be omitted when false, got: %s", b)
		}
	}
}
