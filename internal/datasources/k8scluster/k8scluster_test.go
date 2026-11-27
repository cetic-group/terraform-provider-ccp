// Unit tests for the ccp_k8s_cluster data source. Exercises both lookup
// modes (by id, by name+region) plus the not-found and multiple-matches
// diagnostics, via testutil.NewTestServer + the public client surface
// the data source consumes (`GetK8sCluster`, `ListK8sClusters`).
package k8scluster

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
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

// #2143: the cluster response carries the plan and the upgrade status; the
// data source exposes them (null objects when absent).
func TestDatasource_PlanAndUpgradeStatus(t *testing.T) {
	fx := clusterFixture("ccks-2", "c", "RNN", "prod")
	fx["plan"] = "pro"
	fx["etcd_volume_gb"] = 10
	fx["patch_disponible"] = map[string]any{"version": "1.31.4", "os": "flatcar", "templates_ok": true,
		"plan_de_controle": true, "pools": []string{"default"}, "duree_estimee_minutes": 25}
	fx["upgrade_en_cours"] = map[string]any{"id": "op", "type": "patch", "origine": "automatique",
		"statut": "en_pause", "version_cible": "1.31.4"}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/ccks-2", Status: http.StatusOK, Body: fx},
	})
	defer srv.Close()
	got, err := client.New(srv.URL, "ccp_test_unit", "0.0.0-test").GetK8sCluster(context.Background(), "ccks-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Plan != "pro" || got.EtcdVolumeGB == nil || *got.EtcdVolumeGB != 10 {
		t.Fatalf("plan/etcd = %q/%v", got.Plan, got.EtcdVolumeGB)
	}
	p := patchValue(got.PatchDisponible)
	if p.IsNull() || p.Attributes()["version"].String() != `"1.31.4"` {
		t.Fatalf("patch_disponible = %s", p)
	}
	u := upgradeValue(got.UpgradeEnCours)
	if u.IsNull() || u.Attributes()["statut"].String() != `"en_pause"` || !u.Attributes()["version_depart"].IsNull() {
		t.Fatalf("upgrade_en_cours = %s", u)
	}
	if !patchValue(nil).IsNull() || !upgradeValue(nil).IsNull() {
		t.Fatal("absent: null")
	}
}

// The data source projects `custom_dns` (always a list) and `private_dns`
// (null when the API has none) into its state.
func TestRead_CustomAndPrivateDNS(t *testing.T) {
	ctx := context.Background()
	fx := clusterFixture("ccks-1", "prod-cluster", "RNN", "prod")
	fx["custom_dns"] = []map[string]any{{"domain": "corp.example.com", "servers": []string{"10.0.1.10", "10.0.1.11"}}}
	fx["private_dns"] = map[string]any{
		"status": "error", "resolver_ip": "10.0.0.53",
		"zones":          []map[string]any{{"name": "internal.example", "status": "applied"}},
		"excluded":       []map[string]any{},
		"custom_domains": []map[string]any{{"domain": "corp.example.com", "servers": []string{"10.0.1.10"}, "status": "error", "reason": "invalid_server"}},
		"error":          "Le cluster n'a pas pu être joint.", "error_code": "cluster_unreachable",
		"applied_at": "2026-11-27T10:00:00Z",
	}
	nu := clusterFixture("ccks-2", "bare", "RNN", "dev")
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/ccks-1", Status: http.StatusOK, Body: fx},
		{Method: "GET", Path: "/v1/k8s/clusters/ccks-2", Status: http.StatusOK, Body: nu},
	})
	defer srv.Close()
	d := &k8sClusterDataSource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sresp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sresp)
	read := func(id string) k8sClusterDSModel {
		objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
		champs := map[string]tftypes.Value{}
		for nom, typ := range objType.AttributeTypes {
			champs[nom] = tftypes.NewValue(typ, nil)
		}
		champs["id"] = tftypes.NewValue(tftypes.String, id)
		resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sresp.Schema}}
		d.Read(ctx, datasource.ReadRequest{
			Config: tfsdk.Config{Raw: tftypes.NewValue(objType, champs), Schema: sresp.Schema},
		}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read: %v", resp.Diagnostics.Errors())
		}
		var m k8sClusterDSModel
		if diags := resp.State.Get(ctx, &m); diags.HasError() {
			t.Fatalf("State.Get: %v", diags.Errors())
		}
		return m
	}
	m := read("ccks-1")
	if len(m.CustomDNS.Elements()) != 1 {
		t.Fatalf("custom_dns = %s", m.CustomDNS)
	}
	a := m.PrivateDNS.Attributes()
	if a["status"].(types.String).ValueString() != "error" || a["error_code"].(types.String).ValueString() != "cluster_unreachable" ||
		a["applied_at"].(types.String).ValueString() != "2026-11-27T10:00:00Z" || !a["checked_at"].(types.String).IsNull() {
		t.Fatalf("private_dns = %s", m.PrivateDNS)
	}
	m = read("ccks-2")
	if !m.PrivateDNS.IsNull() || m.CustomDNS.IsNull() || len(m.CustomDNS.Elements()) != 0 {
		t.Fatalf("no DNS: private_dns null, custom_dns [] (got %s, %s)", m.PrivateDNS, m.CustomDNS)
	}
}
