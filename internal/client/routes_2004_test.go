package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeAPI sert des réponses fixes par « MÉTHODE chemin » et note les appels.
// Une route absente répond 404, comme l'API pour une route inexistante.
func fakeAPI(t *testing.T, routes map[string]any) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		calls = append(calls, key)
		body, ok := routes[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "ccp_live_test", "test"), &calls
}

// cetic-cloud-platform#2004 : pas de GET /v1/members/{id} ; lecture dans la liste.
func TestGetOrgMemberLitLaListe(t *testing.T) {
	c, calls := fakeAPI(t, map[string]any{
		"GET /v1/members": []map[string]any{
			{"id": "m1", "email": "a@b.fr", "role": "member", "created_at": "2026-11-21T10:00:00Z"},
			{"id": "m2", "email": "c@d.fr", "role": "admin", "created_at": "2026-11-21T10:00:00Z"},
		},
	})
	m, err := c.GetOrgMember(context.Background(), "m2")
	if err != nil || m.Role != "admin" {
		t.Fatalf("GetOrgMember = %+v, %v", m, err)
	}
	if (*calls)[0] != "GET /v1/members" {
		t.Fatalf("appel = %v, attendu la liste", *calls)
	}
	if _, err := c.GetOrgMember(context.Background(), "absent"); !IsNotFound(err) {
		t.Fatalf("membre absent : erreur %v, attendu 404", err)
	}
}

// La création répond `{email, memberships: [...]}`.
func TestCreateOrgMemberLitLesMemberships(t *testing.T) {
	c, _ := fakeAPI(t, map[string]any{
		"POST /v1/members": map[string]any{
			"email": "a@b.fr",
			"memberships": []map[string]any{
				{"id": "m1", "email": "a@b.fr", "role": "member", "created_at": "2026-11-21T10:00:00Z"},
			},
		},
	})
	m, err := c.CreateOrgMember(context.Background(), OrgMemberCreateRequest{Email: "a@b.fr", Role: "member"})
	if err != nil || m.ID != "m1" {
		t.Fatalf("CreateOrgMember = %+v, %v", m, err)
	}
}

// Pas de GET /v1/quotas/requests/{id} ; lecture dans la liste.
func TestGetQuotaRequestLitLaListe(t *testing.T) {
	c, _ := fakeAPI(t, map[string]any{
		"GET /v1/quotas/requests": []map[string]any{
			{"id": "q1", "field": "max_vms", "requested_value": 10, "status": "pending",
				"created_at": "2026-11-21T10:00:00Z"},
		},
	})
	q, err := c.GetQuotaRequest(context.Background(), "q1")
	if err != nil || q.Field != "max_vms" {
		t.Fatalf("GetQuotaRequest = %+v, %v", q, err)
	}
	if _, err := c.GetQuotaRequest(context.Background(), "absent"); !IsNotFound(err) {
		t.Fatalf("demande absente : erreur %v, attendu 404", err)
	}
}

// La création de ticket répond `{ticket: …}` ; la clôture passe par /close.
func TestSupportTicketCreateEtCloture(t *testing.T) {
	c, calls := fakeAPI(t, map[string]any{
		"POST /v1/support/tickets": map[string]any{
			"ticket":               map[string]any{"id": "t1", "subject": "Aide", "status": "open", "created_at": "2026-11-21T10:00:00Z"},
			"max_priority_allowed": "normal",
		},
		"POST /v1/support/tickets/t1/close": map[string]any{"id": "t1", "status": "closed"},
	})
	tk, err := c.CreateSupportTicket(context.Background(), SupportTicketCreateRequest{
		Subject: "Aide", Body: "x", Category: "other", Priority: "normal"})
	if err != nil || tk.ID != "t1" {
		t.Fatalf("CreateSupportTicket = %+v, %v", tk, err)
	}
	if err := c.CloseSupportTicket(context.Background(), "t1"); err != nil {
		t.Fatalf("CloseSupportTicket : %v", err)
	}
	if (*calls)[len(*calls)-1] != "POST /v1/support/tickets/t1/close" {
		t.Fatalf("appels = %v", *calls)
	}
}
