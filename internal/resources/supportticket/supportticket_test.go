package supportticket

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ticketEtat(t *testing.T, sujet string) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	New().Schema(ctx, resource.SchemaRequest{}, &sr)
	m := ticketResourceModel{
		ID: types.StringValue("t-1"), Subject: types.StringValue(sujet), Body: types.StringValue("x"),
		Category: types.StringValue("technical"), Priority: types.StringValue("normal"),
		Status: types.StringValue("open"), CreatedAt: types.StringValue("2026-11-03T10:00:00Z"),
	}
	st := tfsdk.State{Schema: sr.Schema}
	st.Set(ctx, &m)
	return st
}

func ticketAPI(sujet string) map[string]any {
	return map[string]any{"id": "t-1", "subject": sujet, "category": "technical", "priority": "normal",
		"status": "open", "created_at": "2026-11-03T10:00:00Z", "updated_at": "2026-11-03T10:00:00Z"}
}

func lireSujet(t *testing.T, sujetEtat, sujetAPI string) string {
	t.Helper()
	ctx := context.Background()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/support/tickets/t-1", Status: http.StatusOK, Body: ticketAPI(sujetAPI)},
	})
	defer srv.Close()
	st := ticketEtat(t, sujetEtat)
	r := &ticketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	resp := &resource.ReadResponse{State: st}
	r.Read(ctx, resource.ReadRequest{State: st}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read : %v", resp.Diagnostics)
	}
	var final ticketResourceModel
	resp.State.Get(ctx, &final)
	return final.Subject.ValueString()
}

// Revue de #121 : l'API enregistre le sujet sans espaces autour. Un sujet en
// heredoc (`"Panne\n"`) ne doit pas être réécrit par Read, sinon `subject`
// (RequiresReplace) ferme et rouvre le ticket à chaque apply.
func TestReadGardeLeSujetDeLEtatSiMemeSujet(t *testing.T) {
	if got := lireSujet(t, "Panne\n", "Panne"); got != "Panne\n" {
		t.Fatalf("sujet = %q, attendu celui de l'état", got)
	}
	// Un sujet réellement différent (modifié côté plateforme) est repris.
	if got := lireSujet(t, "Panne\n", "Autre"); got != "Autre" {
		t.Fatalf("sujet = %q, attendu celui de l'API", got)
	}
}

func supprimer(t *testing.T, statut int) resource.DeleteResponse {
	t.Helper()
	ctx := context.Background()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/support/tickets/t-1/close", Status: statut, Body: map[string]any{}},
	})
	defer srv.Close()
	st := ticketEtat(t, "Panne")
	r := &ticketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	resp := resource.DeleteResponse{State: st}
	r.Delete(ctx, resource.DeleteRequest{State: st}, &resp)
	return resp
}

// Le destroy clôture le ticket ; un ticket introuvable est déjà sorti ; une
// autre erreur remonte (suggestion de la review de #121).
func TestDeleteClotureLeTicket(t *testing.T) {
	if r := supprimer(t, http.StatusOK); r.Diagnostics.HasError() {
		t.Fatalf("200 : %v", r.Diagnostics)
	}
	if r := supprimer(t, http.StatusNotFound); r.Diagnostics.HasError() {
		t.Fatalf("404 doit être toléré : %v", r.Diagnostics)
	}
	if r := supprimer(t, http.StatusInternalServerError); !r.Diagnostics.HasError() {
		t.Fatal("500 doit remonter une erreur")
	}
}
