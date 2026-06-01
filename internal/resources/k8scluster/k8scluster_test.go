// Tests for ccp_k8s_cluster — focuses on the v0.21 CCKS HA contract :
// `tier` is Optional+Computed+Default("dev")+OneOf("dev","prod") with
// RequiresReplace (immutable on the backend). The 3 sibling HA fields
// (`proxy_secondary_vmid`, `proxy_secondary_node`, `proxy_vip_vnet`) are
// Computed-only and only populated for tier=prod.
//
// These are unit tests against the typed client and the resource Schema()
// builder. Full acceptance tests (TF_ACC=1) require a live CCP API and are
// out of scope here — the tier wiring at the HTTP layer is the critical
// surface, plus the schema metadata Terraform consumers depend on.
package k8scluster

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// activeBody is a minimal GET response that surfaces tier=dev / tier=prod
// plus all required base fields so json decode succeeds.
func activeBody(id, name, tier string, withProxyHA bool) map[string]any {
	body := map[string]any{
		"id":                                  id,
		"name":                                name,
		"region":                              "RNN",
		"k8s_version":                         "1.31",
		"os_template_key":                     "ccks-capi-debian-13",
		"vpc_id":                              "vpc-1",
		"vnet_id":                             "vnet-1",
		"pod_cidr":                            "10.244.0.0/16",
		"service_cidr":                        "10.96.0.0/12",
		"autoscaler_scale_down_delay_after_add": "10m",
		"autoscaler_scale_down_unneeded_time":   "10m",
		"ingress_controller_enabled":          true,
		"ingress_controller_scope":            "internal",
		"ingress_controller_class":            "incluster",
		"tier":                                tier,
		"status":                              "active",
		"tags":                                []string{},
		"created_at":                          "2026-05-25T10:00:00Z",
		"updated_at":                          "2026-05-25T10:00:00Z",
	}
	if withProxyHA {
		body["proxy_secondary_vmid"] = 12345
		body["proxy_secondary_node"] = "pmx-02"
		body["proxy_vip_vnet"] = "10.1.0.250"
	}
	return body
}

// TestCreate_TierDev asserts that omitting `tier` in the create request body
// still results in a coherent client model (server stamps "dev").
func TestCreate_TierDev(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/k8s/clusters", Status: http.StatusCreated, Body: activeBody("cl-1", "dev-cluster", "dev", false)},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateK8sCluster(context.Background(), client.K8sClusterCreateRequest{
		Name:                     "dev-cluster",
		Region:                   "RNN",
		K8sVersion:               "1.31",
		OsTemplateKey:            "ccks-capi-debian-13",
		VpcID:                    "vpc-1",
		VnetID:                   "vnet-1",
		InitialPool:              client.K8sInitialPool{Name: "default", Plan: "small", Replicas: 1},
		IngressControllerEnabled: true,
		// Tier omitted — backend will stamp "dev" via API default.
	})
	if err != nil {
		t.Fatalf("CreateK8sCluster: %v", err)
	}
	if got.Tier != "dev" {
		t.Fatalf("expected tier=dev, got %q", got.Tier)
	}
	if got.ProxySecondaryVmid != nil || got.ProxySecondaryNode != nil || got.ProxyVipVnet != nil {
		t.Errorf("dev tier should leave proxy_secondary_* and proxy_vip_vnet null, got vmid=%v node=%v vip=%v",
			got.ProxySecondaryVmid, got.ProxySecondaryNode, got.ProxyVipVnet)
	}

	// When the caller leaves Tier empty, the JSON body must omit the field
	// (json tag `tier,omitempty`) — the API then applies its own default.
	calls := srv.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(calls))
	}
	if strings.Contains(string(calls[0].Body), `"tier"`) {
		t.Errorf("create body should omit tier when caller leaves it empty, got %s", string(calls[0].Body))
	}
}

// TestCreate_TierProd asserts that an explicit "prod" tier is forwarded in
// the JSON body verbatim and that the HA proxy fields propagate from the
// API response into the typed model.
func TestCreate_TierProd(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/k8s/clusters", Status: http.StatusCreated, Body: activeBody("cl-2", "prod-cluster", "prod", true)},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateK8sCluster(context.Background(), client.K8sClusterCreateRequest{
		Name:                     "prod-cluster",
		Region:                   "RNN",
		K8sVersion:               "1.31",
		OsTemplateKey:            "ccks-capi-debian-13",
		VpcID:                    "vpc-1",
		VnetID:                   "vnet-1",
		InitialPool:              client.K8sInitialPool{Name: "default", Plan: "medium", Replicas: 2},
		IngressControllerEnabled: true,
		Tier:                     "prod",
	})
	if err != nil {
		t.Fatalf("CreateK8sCluster(tier=prod): %v", err)
	}
	if got.Tier != "prod" {
		t.Fatalf("expected tier=prod, got %q", got.Tier)
	}
	if got.ProxySecondaryVmid == nil || *got.ProxySecondaryVmid != 12345 {
		t.Errorf("expected proxy_secondary_vmid=12345, got %v", got.ProxySecondaryVmid)
	}
	if got.ProxySecondaryNode == nil || *got.ProxySecondaryNode != "pmx-02" {
		t.Errorf("expected proxy_secondary_node=pmx-02, got %v", got.ProxySecondaryNode)
	}
	if got.ProxyVipVnet == nil || *got.ProxyVipVnet != "10.1.0.250" {
		t.Errorf("expected proxy_vip_vnet=10.1.0.250, got %v", got.ProxyVipVnet)
	}

	var sent struct {
		Tier string `json:"tier"`
	}
	if err := json.Unmarshal(srv.Calls()[0].Body, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if sent.Tier != "prod" {
		t.Errorf("expected request body tier=prod, got %q", sent.Tier)
	}
}

// TestRead_LegacyClusterMissingTier verifies that clusters created before
// the HA backend migration (no `tier` in the JSON response) don't break the
// client — Go's JSON decoder leaves the field empty, and stateFromAPI
// collapses it to the default ("dev").
func TestRead_LegacyClusterMissingTier(t *testing.T) {
	body := map[string]any{
		"id":                                  "cl-legacy",
		"name":                                "old-cluster",
		"region":                              "RNN",
		"k8s_version":                         "1.30",
		"os_template_key":                     "ccks-capi-debian-13",
		"vpc_id":                              "vpc-1",
		"vnet_id":                             "vnet-1",
		"pod_cidr":                            "10.244.0.0/16",
		"service_cidr":                        "10.96.0.0/12",
		"autoscaler_scale_down_delay_after_add": "10m",
		"autoscaler_scale_down_unneeded_time":   "10m",
		"ingress_controller_enabled":          true,
		"ingress_controller_scope":            "internal",
		"ingress_controller_class":            "incluster",
		// no `tier`, no proxy_* fields
		"status":     "active",
		"tags":       []string{},
		"created_at": "2025-09-01T00:00:00Z",
		"updated_at": "2025-09-01T00:00:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-legacy", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetK8sCluster(context.Background(), "cl-legacy")
	if err != nil {
		t.Fatalf("GetK8sCluster: %v", err)
	}
	if got.Tier != "" {
		t.Errorf("legacy row should decode with empty tier (collapsed by resource Read), got %q", got.Tier)
	}

	// Drive the legacy row through stateFromAPI and confirm it surfaces "dev".
	state, _ := stateFromAPI(context.Background(), got, nil)
	if state.Tier.ValueString() != "dev" {
		t.Errorf("stateFromAPI should collapse missing tier to default \"dev\", got %q", state.Tier.ValueString())
	}
	if !state.ProxySecondaryVmid.IsNull() || !state.ProxySecondaryNode.IsNull() || !state.ProxyVipVnet.IsNull() {
		t.Errorf("legacy/dev tier should leave proxy_* state attrs null")
	}
}

// TestSchema_TierForceNew asserts the `tier` attribute is Optional+Computed
// with a Default and carries a RequiresReplace plan modifier — backend
// cannot mutate the tier of an existing cluster (proxy LXC topology is
// baked at provision time).
func TestSchema_TierForceNew(t *testing.T) {
	r := New().(*k8sResource)
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema build errors: %v", resp.Diagnostics.Errors())
	}
	attr, ok := resp.Schema.Attributes["tier"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("tier attribute missing or wrong type: %T", resp.Schema.Attributes["tier"])
	}
	if !attr.Optional || !attr.Computed {
		t.Errorf("tier should be Optional+Computed, got optional=%v computed=%v", attr.Optional, attr.Computed)
	}
	if attr.Default == nil {
		t.Errorf("tier should have a Default (\"dev\")")
	}
	if len(attr.Validators) == 0 {
		t.Errorf("tier should carry an OneOf(\"dev\",\"prod\") validator")
	}
	// Match on "destroy and recreate" rather than "replace" — same convention
	// as the sshkey scope test (the canonical framework phrasing).
	foundRequiresReplace := false
	for _, pm := range attr.PlanModifiers {
		desc := strings.ToLower(pm.Description(context.Background()))
		if strings.Contains(desc, "destroy and recreate") || strings.Contains(desc, "replace") {
			foundRequiresReplace = true
			break
		}
	}
	if !foundRequiresReplace {
		t.Errorf("tier should carry RequiresReplace() — backend cannot mutate tier on an existing cluster")
	}
}

// TestSchema_ProxyHAFieldsComputed asserts the 3 HA proxy attributes are
// Computed-only (not user-settable).
func TestSchema_ProxyHAFieldsComputed(t *testing.T) {
	r := New().(*k8sResource)
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	// proxy_secondary_vmid is Int64
	vmid, ok := resp.Schema.Attributes["proxy_secondary_vmid"].(schema.Int64Attribute)
	if !ok {
		t.Fatalf("proxy_secondary_vmid missing or wrong type: %T", resp.Schema.Attributes["proxy_secondary_vmid"])
	}
	if vmid.Required || vmid.Optional || !vmid.Computed {
		t.Errorf("proxy_secondary_vmid must be Computed-only, got required=%v optional=%v computed=%v",
			vmid.Required, vmid.Optional, vmid.Computed)
	}

	for _, name := range []string{"proxy_secondary_node", "proxy_vip_vnet"} {
		attr, ok := resp.Schema.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s missing or wrong type: %T", name, resp.Schema.Attributes[name])
		}
		if attr.Required || attr.Optional || !attr.Computed {
			t.Errorf("%s must be Computed-only, got required=%v optional=%v computed=%v",
				name, attr.Required, attr.Optional, attr.Computed)
		}
	}
}
