package k8scluster

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func sv(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// The planning rules of `plan` / `tier` (plan.go), configurations written
// before #2143 first: they must plan no change on an existing cluster.
func TestEffectivePlan(t *testing.T) {
	cases := []struct {
		name                 string
		cfgPlan, cfgTier     string
		statePlan, stateTier string
		stateExists          bool
		wantPlan, wantTier   string
		wantReplace          bool
	}{
		// ── compatibility: configurations written before #2143 ──
		{"old config tier=prod, refreshed state standard", "", "prod", "standard", "prod", true, "standard", "prod", false},
		{"old config tier=prod, state pro (changed in the console)", "", "prod", "pro", "prod", true, "pro", "prod", false},
		{"old config tier=prod, state from old provider (no plan)", "", "prod", "", "prod", true, "standard", "prod", false},
		{"old config without tier, state dev", "", "", "starter", "dev", true, "starter", "dev", false},
		{"old config without tier, state from old provider", "", "", "", "dev", true, "starter", "dev", false},
		{"old config tier=dev", "", "dev", "starter", "dev", true, "starter", "dev", false},
		// ── creation ──
		{"create, nothing set", "", "", "", "", false, "starter", "dev", false},
		{"create tier=prod", "", "prod", "", "", false, "standard", "prod", false},
		{"create plan=pro", "pro", "", "", "", false, "pro", "prod", false},
		{"create plan=pro tier=prod", "pro", "prod", "", "", false, "pro", "prod", false},
		// ── changes ──
		{"standard → pro in place", "pro", "", "standard", "prod", true, "pro", "prod", false},
		{"pro → standard in place", "standard", "", "pro", "prod", true, "standard", "prod", false},
		{"tier=prod → plan=pro (migration) in place", "pro", "", "standard", "prod", true, "pro", "prod", false},
		{"starter → standard replaces", "standard", "", "starter", "dev", true, "standard", "prod", true},
		{"pro → starter replaces", "starter", "", "pro", "prod", true, "starter", "dev", true},
		{"tier dev → prod replaces", "", "prod", "starter", "dev", true, "standard", "prod", true},
		{"plan removed from config keeps the state plan", "", "", "pro", "prod", true, "pro", "prod", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := planInputs{
				cfgPlan: sv(c.cfgPlan), cfgTier: sv(c.cfgTier),
				statePlan: sv(c.statePlan), stateTier: sv(c.stateTier),
				stateExists: c.stateExists,
			}
			got, known := in.effective()
			if !known || got != c.wantPlan {
				t.Fatalf("plan = %q (known %v), want %q", got, known, c.wantPlan)
			}
			if tierOf(got) != c.wantTier {
				t.Fatalf("tier = %q, want %q", tierOf(got), c.wantTier)
			}
			if in.requiresReplace() != c.wantReplace {
				t.Fatalf("requiresReplace = %v, want %v", in.requiresReplace(), c.wantReplace)
			}
		})
	}
	// A configuration not known yet stays unknown, without a replacement.
	in := planInputs{cfgPlan: types.StringUnknown(), statePlan: sv("starter"), stateExists: true}
	if _, known := in.effective(); known || in.requiresReplace() {
		t.Fatal("unknown configured plan must stay unknown and not force a replacement")
	}
}

func activeV2143(plan string) map[string]any {
	b := activeBody("cl-1", "c", tierOf(plan), plan != "starter")
	b["plan"] = plan
	b["k8s_version"] = "1.34.9"
	b["os_template_key"] = "kube-v1-34-9"
	b["etcd_volume_gb"] = 5
	b["node_ip_block_size"] = 176
	return b
}

// stateFromAPI after an automatic patch and a version upgrade by the
// platform: the desired version and the template key stay the configured
// ones (no diff, no replacement), the actual version is exposed.
func TestStateFromAPI_KeepsDesiredVersionAndTemplate(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Status: http.StatusOK, Body: activeV2143("pro")},
	})
	defer srv.Close()
	got, err := client.New(srv.URL, "k", "test").GetK8sCluster(context.Background(), "cl-1")
	if err != nil {
		t.Fatal(err)
	}
	prior := &k8sResourceModel{
		K8sVersion:    types.StringValue("v1.34.8"),
		OsTemplateKey: types.StringValue("kube-v1-34-8"),
		Tier:          types.StringValue("prod"),
	}
	m, _ := stateFromAPI(context.Background(), got, prior)
	if m.K8sVersion.ValueString() != "v1.34.8" {
		t.Errorf("k8s_version = %s, want the configured v1.34.8", m.K8sVersion)
	}
	if m.CurrentK8sVersion.ValueString() != "1.34.9" {
		t.Errorf("current_k8s_version = %s", m.CurrentK8sVersion)
	}
	if m.OsTemplateKey.ValueString() != "kube-v1-34-8" {
		t.Errorf("os_template_key = %s, the rewrite by the platform must not show (it forces replacement)", m.OsTemplateKey)
	}
	if m.Plan.ValueString() != "pro" || m.Tier.ValueString() != "prod" {
		t.Errorf("plan/tier = %s/%s", m.Plan, m.Tier)
	}
	if m.EtcdVolumeGB.ValueInt64() != 5 || m.NodeIPBlockSize.ValueInt64() != 176 {
		t.Errorf("etcd/ip block = %s/%s", m.EtcdVolumeGB, m.NodeIPBlockSize)
	}
	if !m.OIDC.IsNull() || !m.Maintenance.IsNull() {
		t.Errorf("oidc/maintenance should be null here")
	}
	// Import (no prior): the API values.
	m, _ = stateFromAPI(context.Background(), got, nil)
	if m.K8sVersion.ValueString() != "1.34.9" || m.OsTemplateKey.ValueString() != "kube-v1-34-9" {
		t.Errorf("import: %s / %s", m.K8sVersion, m.OsTemplateKey)
	}
}

// The create request carries `plan` and its equivalent `tier` (accepted
// together by the API when they agree; an API older than #2143 still gets
// the right frontend topology).
func TestCreateRequest_PlanAndTier(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/k8s/clusters", Status: http.StatusCreated, Body: activeV2143("pro")},
	})
	defer srv.Close()
	_, err := client.New(srv.URL, "k", "test").CreateK8sCluster(context.Background(), client.K8sClusterCreateRequest{
		Name: "c", Region: "RNN", K8sVersion: "v1.34.8", Plan: "pro", Tier: tierOf("pro"),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(srv.Calls()[0].Body)
	for _, want := range []string{`"plan":"pro"`, `"tier":"prod"`} {
		if !contains(body, want) {
			t.Errorf("create body %s lacks %s", body, want)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
