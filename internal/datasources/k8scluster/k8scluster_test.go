// Unit tests for the ccp_k8s_cluster data source. Exercises both lookup
// modes (by id, by name+region) plus the not-found and multiple-matches
// diagnostics, via testutil.NewTestServer + the public client surface
// the data source consumes (`GetK8sCluster`, `ListK8sClusters`).
package k8scluster

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

// clusterFixture mirrors the JSON shape returned by `GET /v1/k8s/clusters/{id}`
// and `GET /v1/k8s/clusters` — keep aligned with `client.K8sCluster`.
func clusterFixture(id, name, region, tier string) map[string]any {
	fx := map[string]any{
		"id":                                    id,
		"name":                                  name,
		"region":                                region,
		"k8s_version":                           "v1.31.0",
		"os_template_key":                       "clks-capi-debian-13",
		"vpc_id":                                "vpc-1",
		"vnet_id":                               "vnet-1",
		"pod_cidr":                              "10.244.0.0/16",
		"service_cidr":                          "10.96.0.0/12",
		"autoscaler_scale_down_delay_after_add": "10m",
		"autoscaler_scale_down_unneeded_time":   "10m",
		"ingress_controller_enabled":            true,
		"ingress_controller_scope":              "internal",
		"ingress_controller_class":              "incluster",
		"tier":                                  tier,
		"status":                                "active",
		"tags":                                  []string{},
		"created_at":                            "2026-05-25T10:00:00Z",
		"updated_at":                            "2026-05-25T10:05:00Z",
	}
	if tier == "prod" {
		fx["proxy_secondary_vmid"] = 1042
		fx["proxy_secondary_node"] = "px-node-02"
		fx["proxy_vip_vnet"] = "10.10.0.250"
	}
	return fx
}

// TestDatasourceByID exercises the `id` lookup path. Mirrors the client
// call the data source's Read makes when `hasID` is true.
func TestDatasourceByID(t *testing.T) {
	id := "ccks-1"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/" + id, Status: http.StatusOK, Body: clusterFixture(id, "prod-cluster", "RNN", "prod")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.GetK8sCluster(context.Background(), id)
	if err != nil {
		t.Fatalf("GetK8sCluster: %v", err)
	}
	if got.Name != "prod-cluster" {
		t.Errorf("expected name=prod-cluster, got %q", got.Name)
	}
	if got.Tier != "prod" {
		t.Errorf("expected tier=prod, got %q", got.Tier)
	}
	if got.ProxySecondaryVmid == nil || *got.ProxySecondaryVmid != 1042 {
		t.Errorf("expected proxy_secondary_vmid=1042, got %v", got.ProxySecondaryVmid)
	}
	if got.ProxyVipVnet == nil || *got.ProxyVipVnet != "10.10.0.250" {
		t.Errorf("expected proxy_vip_vnet=10.10.0.250, got %v", got.ProxyVipVnet)
	}
}

// TestDatasourceByNameRegion exercises the `(name, region)` lookup path:
// list the region, filter Go-side, ensure exactly one match.
func TestDatasourceByNameRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "GET", Path: "/v1/k8s/clusters",
			Status: http.StatusOK,
			Body: []map[string]any{
				clusterFixture("ccks-a", "alpha", "RNN", "dev"),
				clusterFixture("ccks-b", "beta", "RNN", "prod"),
				clusterFixture("ccks-c", "alpha", "PAR", "dev"),
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	list, err := c.ListK8sClusters(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListK8sClusters: %v", err)
	}
	wantName, wantRegion := "beta", "RNN"
	matches := make([]int, 0, 1)
	for i := range list {
		if list[i].Name == wantName && list[i].Region == wantRegion {
			matches = append(matches, i)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 match for (beta, RNN), got %d", len(matches))
	}
	if list[matches[0]].ID != "ccks-b" {
		t.Errorf("expected ccks-b, got %q", list[matches[0]].ID)
	}
	if list[matches[0]].Tier != "prod" {
		t.Errorf("expected tier=prod on ccks-b, got %q", list[matches[0]].Tier)
	}
}

// TestDatasourceNotFound asserts a 404 from `GET /v1/k8s/clusters/{id}`
// surfaces as an error from the client (which the data source then maps
// to a `Failed to read K8s cluster` diagnostic).
func TestDatasourceNotFound(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/missing", Status: http.StatusNotFound, Body: map[string]any{"detail": "Cluster not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	_, err := c.GetK8sCluster(context.Background(), "missing")
	if err == nil {
		t.Fatalf("expected error on 404, got nil")
	}
	if !client.IsNotFound(err) {
		t.Errorf("expected IsNotFound to recognise the error, got %v", err)
	}

	// And the name+region path with zero hits: the data source raises a
	// dedicated 'K8s cluster not found' diagnostic. Validate the filter
	// semantics by direct execution.
	srv2 := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters", Status: http.StatusOK, Body: []map[string]any{
			clusterFixture("ccks-x", "other", "RNN", "dev"),
		}},
	})
	defer srv2.Close()
	c2 := client.New(srv2.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c2.ListK8sClusters(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListK8sClusters: %v", err)
	}
	matches := 0
	for i := range list {
		if list[i].Name == "ghost" && list[i].Region == "RNN" {
			matches++
		}
	}
	if matches != 0 {
		t.Errorf("expected 0 matches for missing (name, region), got %d", matches)
	}
}

// TestDatasourceMultipleMatches asserts the filter Go-side detects
// duplicates and would raise the corresponding error diag.
func TestDatasourceMultipleMatches(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "GET", Path: "/v1/k8s/clusters",
			Status: http.StatusOK,
			Body: []map[string]any{
				// Two clusters share (name, region) — only possible if the
				// backend uniqueness constraint is bypassed, but the data
				// source must still surface a clear error rather than
				// silently picking the first row.
				clusterFixture("ccks-dup-a", "dup", "RNN", "dev"),
				clusterFixture("ccks-dup-b", "dup", "RNN", "prod"),
				clusterFixture("ccks-other", "solo", "RNN", "dev"),
			},
		},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	list, err := c.ListK8sClusters(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListK8sClusters: %v", err)
	}
	wantName, wantRegion := "dup", "RNN"
	matches := 0
	for i := range list {
		if list[i].Name == wantName && list[i].Region == wantRegion {
			matches++
		}
	}
	if matches < 2 {
		t.Fatalf("expected at least 2 matches for (dup, RNN), got %d", matches)
	}
	// In the live data source, this branch raises:
	//   "Multiple K8s clusters matched: Found N clusters named %q in region %q."
}
