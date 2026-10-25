package dbpgextensions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

func TestCatalogIsReadForTheRequestedVersion(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/db/pg/extensions" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": 16,
			"extensions": []map[string]any{
				{"name": "postgis_topology", "description": "Topologie", "requires_preload": false,
					"requires": []string{"postgis"}, "single_database": false},
			},
			"preload_libraries": []string{"pg_cron"},
			"direct_extensions": []string{"pgcrypto"},
		})
	}))
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	cat, err := c.GetDbPgExtensionCatalog(context.Background(), 16)
	if err != nil {
		t.Fatalf("GetDbPgExtensionCatalog: %v", err)
	}
	if query != "version=16" {
		t.Errorf("query = %q", query)
	}
	if len(cat.Extensions) != 1 || cat.Extensions[0].Requires[0] != "postgis" ||
		cat.DirectExtensions[0] != "pgcrypto" || cat.PreloadLibraries[0] != "pg_cron" {
		t.Errorf("unexpected catalog: %+v", cat)
	}
}

func TestUnknownVersionIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"Aucune extension n'est proposée pour PostgreSQL 15."}`))
	}))
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	if _, err := c.GetDbPgExtensionCatalog(context.Background(), 15); !client.IsNotFound(err) {
		t.Fatalf("err = %v, want not found", err)
	}
}
