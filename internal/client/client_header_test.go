package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDoSetsXCCPClientHeader vérifie que chaque requête porte X-CCP-Client: terraform
// (origine déterministe pour l'audit trail plateforme) en plus du User-Agent.
func TestDoSetsXCCPClientHeader(t *testing.T) {
	var gotClient, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClient = r.Header.Get("X-CCP-Client")
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "ccp_live_test", "4.2.1")
	var out map[string]any
	if err := c.do(context.Background(), http.MethodGet, "/v1/regions", nil, &out); err != nil {
		t.Fatalf("do() error: %v", err)
	}
	if gotClient != "terraform" {
		t.Errorf("X-CCP-Client = %q, want %q", gotClient, "terraform")
	}
	if gotUA != "terraform-provider-ccp/4.2.1" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "terraform-provider-ccp/4.2.1")
	}
}
