package client

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Régression : le champ `detail` d'une erreur API peut être une chaîne
// (contrat historique), un objet structuré {"code","message","action_url"}
// (contrat unifié #618) ou une liste (erreurs de validation). parseAPIError
// doit surfacer un message lisible dans les trois cas — en particulier
// extraire `message` d'un objet dict au lieu de rendre du JSON brut à
// l'opérateur Terraform.
func newResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestParseAPIError_StringDetail(t *testing.T) {
	err := parseAPIError(newResp(404, `{"detail":"Ressource introuvable."}`), "GET", "/v1/x")
	ae, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if ae.Detail != "Ressource introuvable." {
		t.Fatalf("got detail %q", ae.Detail)
	}
}

func TestParseAPIError_StructuredDetailPrefersMessage(t *testing.T) {
	body := `{"detail":{"code":"quota_exceeded","message":"Quota dépassé : containers.","action_url":"/billing"}}`
	err := parseAPIError(newResp(429, body), "POST", "/v1/containers")
	ae := err.(*APIError)
	if ae.Detail != "Quota dépassé : containers." {
		t.Fatalf("expected human message, got %q", ae.Detail)
	}
}

func TestParseAPIError_StructuredDetailFallsBackToCode(t *testing.T) {
	body := `{"detail":{"code":"payment_method_required"}}`
	err := parseAPIError(newResp(402, body), "POST", "/v1/containers")
	ae := err.(*APIError)
	if ae.Detail != "payment_method_required" {
		t.Fatalf("expected code fallback, got %q", ae.Detail)
	}
}

func TestParseAPIError_ValidationListStillReEncoded(t *testing.T) {
	body := `{"detail":[{"loc":["body","name"],"msg":"field required"}]}`
	err := parseAPIError(newResp(422, body), "POST", "/v1/containers")
	ae := err.(*APIError)
	if !strings.Contains(ae.Detail, "field required") {
		t.Fatalf("expected validation detail re-encoded, got %q", ae.Detail)
	}
}
