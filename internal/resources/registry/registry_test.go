// Tests unitaires pour ccp_registry — refactor 2026-05-10.
//
// La registry n'est plus une ressource réseau (drop VPC/VNet/PublicIP).
// On exerce le client + les helpers du resource (setState, pollUntilActive)
// avec un backend mocké via testutil.NewTestServer. Tests acceptance complets
// nécessitent TF_ACC=1 + un endpoint CCP réel.
package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// fixturePending : payload registry en cours de provision.
func fixturePending(id string) map[string]any {
	return map[string]any{
		"id":                 id,
		"name":               "ccr-test",
		"slug":               "ccr-test",
		"region":             "RNN",
		"expose_public":      false,
		"expose_private":     true,
		"url":                "https://ccr-test-aabbccdd.registry-rnn.cloud.cetic-group.com",
		"registry_image_tag": "2.8",
		"gc_schedule_cron":   "0 3 * * 0",
		"status":             "provisioning",
		"tags":               []string{},
		"created_at":         "2026-05-09T10:00:00Z",
	}
}

func fixtureActive(id string, overrides map[string]any) map[string]any {
	m := fixturePending(id)
	m["status"] = "active"
	m["admin_username"] = "admin"
	for k, v := range overrides {
		m[k] = v
	}
	return m
}

// ─── 1. Create — privé, polls jusqu'à active ─────────────────────────────────

func TestCreate_Private_PollsToActive(t *testing.T) {
	createdBody := fixturePending("reg-1")
	createBody := map[string]any{}
	for k, v := range createdBody {
		createBody[k] = v
	}
	createBody["admin_username"] = "admin"
	createBody["admin_password"] = "s3cret-once"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/registries", Status: http.StatusCreated, Body: createBody},
		{Method: "GET", Path: "/v1/registries/reg-1", Status: http.StatusOK, Body: fixturePending("reg-1")},
		{Method: "GET", Path: "/v1/registries/reg-1", Status: http.StatusOK, Body: fixturePending("reg-1")},
		{Method: "GET", Path: "/v1/registries/reg-1", Status: http.StatusOK, Body: fixtureActive("reg-1", nil)},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	created, err := c.CreateRegistry(context.Background(), client.RegistryCreateRequest{
		Name:          "ccr-test",
		Region:        "RNN",
		ExposePublic:  false,
		ExposePrivate: true,
	})
	if err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}
	if created.AdminPassword != "s3cret-once" {
		t.Fatalf("admin_password not propagated: got %q", created.AdminPassword)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	final, err := pollUntilActive(ctx, c, "reg-1", 60*time.Second)
	if err != nil {
		t.Fatalf("pollUntilActive: %v", err)
	}
	if final.Status != client.RegistryStatusActive {
		t.Fatalf("expected status=active, got %q", final.Status)
	}
}

// ─── 2. Create — public+privé, body forwarde les flags ───────────────────────

func TestCreate_BothExposures_ForwardsBody(t *testing.T) {
	publicReg := fixtureActive("reg-2", map[string]any{
		"expose_public":  true,
		"expose_private": true,
	})
	createBody := map[string]any{}
	for k, v := range publicReg {
		createBody[k] = v
	}
	createBody["admin_username"] = "admin"
	createBody["admin_password"] = "x"

	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/registries", Status: http.StatusCreated, Body: createBody},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	if _, err := c.CreateRegistry(context.Background(), client.RegistryCreateRequest{
		Name:          "ccr-test",
		Region:        "RNN",
		ExposePublic:  true,
		ExposePrivate: true,
	}); err != nil {
		t.Fatalf("CreateRegistry: %v", err)
	}

	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	var sent map[string]any
	if err := json.Unmarshal(calls[0].Body, &sent); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if sent["expose_public"] != true {
		t.Errorf("body expose_public should be true, got %v", sent["expose_public"])
	}
	if sent["expose_private"] != true {
		t.Errorf("body expose_private should be true, got %v", sent["expose_private"])
	}
}

// ─── 3. Read — drift sur expose_public propagé ───────────────────────────────

func TestRead_DriftOnExposePublic(t *testing.T) {
	drifted := fixtureActive("reg-3", map[string]any{
		"expose_public": true,  // backoffice/admin a togglé
	})
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries/reg-3", Status: http.StatusOK, Body: drifted},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.GetRegistry(context.Background(), "reg-3")
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	if !got.ExposePublic {
		t.Errorf("expected drifted expose_public=true")
	}
	var m registryResourceModel
	setState(context.Background(), &m, got)
	if !m.ExposePublic.ValueBool() {
		t.Errorf("setState drift not propagated")
	}
}

// ─── 4. Update — PATCH expose_public + re-Read ───────────────────────────────

func TestUpdate_TogglesExposure(t *testing.T) {
	updated := fixtureActive("reg-4", map[string]any{
		"expose_public": true,
	})
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/registries/reg-4", Status: http.StatusOK, Body: updated},
		{Method: "GET", Path: "/v1/registries/reg-4", Status: http.StatusOK, Body: updated},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	tt := true
	got, err := c.UpdateRegistry(context.Background(), "reg-4", client.RegistryUpdateRequest{
		ExposePublic: &tt,
	})
	if err != nil {
		t.Fatalf("UpdateRegistry: %v", err)
	}
	if !got.ExposePublic {
		t.Fatalf("expected expose_public=true after update")
	}
	if _, err := c.GetRegistry(context.Background(), "reg-4"); err != nil {
		t.Fatalf("re-Read after update: %v", err)
	}
}

// ─── 5. Delete — 404 silencieux ──────────────────────────────────────────────

func TestDelete_AlreadyGone(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/registries/reg-6", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	err := c.DeleteRegistry(context.Background(), "reg-6")
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound error, got %v", err)
	}
}

// ─── 6. Unmarshal round-trip — shape Registry post-refactor ──────────────────

func TestUnmarshal_RegistryShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"minimal", `{"id":"a","name":"n","slug":"n","region":"RNN","expose_public":false,"expose_private":true,"registry_image_tag":"2.8","gc_schedule_cron":"0 3 * * 0","status":"active","tags":[],"created_at":"2026-05-09T10:00:00Z"}`},
		{"populated", `{"id":"a","name":"n","slug":"n","region":"RNN","expose_public":true,"expose_private":true,"url":"https://x.example","registry_image_tag":"2.8","gc_schedule_cron":"0 3 * * 0","status":"active","admin_username":"admin","storage_used_gb":42,"last_push_at":"2026-05-09T11:00:00Z","tags":["a","b"],"created_at":"2026-05-09T10:00:00Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r client.Registry
			if err := json.NewDecoder(strings.NewReader(tc.raw)).Decode(&r); err != nil {
				t.Fatalf("decode: %v", err)
			}
			var m registryResourceModel
			setState(context.Background(), &m, &r)
			if m.ID.ValueString() != "a" {
				t.Errorf("id mismatch: %q", m.ID.ValueString())
			}
		})
	}
}
