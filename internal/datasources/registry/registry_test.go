// Tests pour le datasource ccp_registry — refactor 2026-05-10.
package registry

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func basic(id, name, region string) map[string]any {
	return map[string]any{
		"id":                 id,
		"name":               name,
		"slug":               name,
		"region":             region,
		"expose_public":      false,
		"expose_private":     true,
		"url":                "https://" + name + "-aabbccdd.registry-" + region + ".cloud.cetic-group.com",
		"registry_image_tag": "2.8",
		"gc_schedule_cron":   "0 3 * * 0",
		"status":             "active",
		"tags":               []string{},
		"created_at":         "2026-05-09T10:00:00Z",
	}
}

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries/reg-1", Status: http.StatusOK, Body: basic("reg-1", "ccr-prod", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetRegistry(context.Background(), "reg-1")
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	if got.URL == nil || *got.URL == "" {
		t.Errorf("expected non-empty URL")
	}
	if !got.ExposePrivate {
		t.Errorf("expected expose_private=true")
	}
}

func TestLookupByNameRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries", Status: http.StatusOK, Body: []map[string]any{
			basic("reg-1", "ccr-prod", "RNN"),
			basic("reg-2", "ccr-staging", "PAR"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListRegistries(context.Background())
	if err != nil {
		t.Fatalf("ListRegistries: %v", err)
	}
	var found *client.Registry
	for i := range list {
		if list[i].Name == "ccr-prod" && list[i].Region == "RNN" {
			found = &list[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected to find ccr-prod in RNN")
	}
	if found.ID != "reg-1" {
		t.Errorf("expected reg-1, got %q", found.ID)
	}
}
