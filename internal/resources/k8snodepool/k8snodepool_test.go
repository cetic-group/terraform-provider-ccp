package k8snodepool

import (
	"context"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ptr(i int) *int { return &i }

// setState doit normaliser l'état "autoscaler désactivé" (max_size 0/null) vers
// null/null, car min_size/max_size sont Optional (non-Computed) : le state final
// doit == la config (null). Sinon : "inconsistent result: was null, but now 0".
func TestSetState_AutoscalerNormalization(t *testing.T) {
	cases := []struct {
		name             string
		min, max         *int
		wantMinNull      bool
		wantMaxNull      bool
		wantMin, wantMax int64
	}{
		{"disabled via 0/0 → null/null", ptr(0), ptr(0), true, true, 0, 0},
		{"disabled via nil/nil → null/null", nil, nil, true, true, 0, 0},
		{"disabled via min set but max 0 → null/null", ptr(2), ptr(0), true, true, 0, 0},
		{"enabled 2/5 → 2/5", ptr(2), ptr(5), false, false, 2, 5},
		{"enabled scale-to-zero 0/5 → 0/5", ptr(0), ptr(5), false, false, 0, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &poolResourceModel{}
			setState(context.Background(), m, &client.K8sNodePool{
				ID: "id", ClusterID: "cid", Name: "n", Plan: "small",
				Replicas: 1, MinSize: tc.min, MaxSize: tc.max, Status: "active",
			})
			if m.MinSize.IsNull() != tc.wantMinNull {
				t.Fatalf("min_size null = %v, want %v", m.MinSize.IsNull(), tc.wantMinNull)
			}
			if m.MaxSize.IsNull() != tc.wantMaxNull {
				t.Fatalf("max_size null = %v, want %v", m.MaxSize.IsNull(), tc.wantMaxNull)
			}
			if !tc.wantMinNull && m.MinSize.ValueInt64() != tc.wantMin {
				t.Fatalf("min_size = %d, want %d", m.MinSize.ValueInt64(), tc.wantMin)
			}
			if !tc.wantMaxNull && m.MaxSize.ValueInt64() != tc.wantMax {
				t.Fatalf("max_size = %d, want %d", m.MaxSize.ValueInt64(), tc.wantMax)
			}
		})
	}
}

func strp(s string) *string { return &s }

// #2143: the platform pins an inheriting pool on an explicit version after a
// control-plane upgrade, and patches pools automatically. k8s_version keeps
// the desired value while the pool runs it or higher (no perpetual diff, no
// downgrade); current_k8s_version is the actual one.
func TestSetState_K8sVersionFloor(t *testing.T) {
	cases := []struct {
		name        string
		prior       types.String
		api         *string
		wantDesired types.String
		wantCurrent types.String
	}{
		{"inherited, now pinned", types.StringNull(), strp("1.33.4"), types.StringValue("1.33.4"), types.StringValue("1.33.4")},
		{"configured, patched above", types.StringValue("v1.34.8"), strp("1.34.9"), types.StringValue("v1.34.8"), types.StringValue("1.34.9")},
		{"configured, same", types.StringValue("v1.34.8"), strp("1.34.8"), types.StringValue("v1.34.8"), types.StringValue("1.34.8")},
		{"inherits", types.StringNull(), nil, types.StringNull(), types.StringNull()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &poolResourceModel{K8sVersion: c.prior}
			setState(context.Background(), m, &client.K8sNodePool{
				ID: "id", ClusterID: "cid", Name: "n", Plan: "small", Replicas: 1,
				K8sVersion: c.api, Status: "active",
			})
			if !m.K8sVersion.Equal(c.wantDesired) || !m.CurrentK8sVersion.Equal(c.wantCurrent) {
				t.Fatalf("k8s_version=%s current=%s, want %s / %s", m.K8sVersion, m.CurrentK8sVersion, c.wantDesired, c.wantCurrent)
			}
		})
	}
}

// disk_gb unset is Computed: unknown in the plan of any other change.
// UseStateForUnknown must run BEFORE RequiresReplace, otherwise a replicas
// change replaces the pool.
func TestSchema_DiskGBStateBeforeReplace(t *testing.T) {
	resp := &resource.SchemaResponse{}
	New().Schema(context.Background(), resource.SchemaRequest{}, resp)
	a := resp.Schema.Attributes["disk_gb"].(schema.Int64Attribute)
	if len(a.PlanModifiers) != 2 {
		t.Fatalf("disk_gb modifiers: %d", len(a.PlanModifiers))
	}
	first := strings.ToLower(a.PlanModifiers[0].Description(context.Background()))
	if strings.Contains(first, "destroy and recreate") || strings.Contains(first, "replace") {
		t.Fatalf("first disk_gb modifier must be UseStateForUnknown, got %q", first)
	}
	if !a.Optional || !a.Computed {
		t.Fatal("disk_gb must be Optional+Computed (server default 20 GB)")
	}
	ver := resp.Schema.Attributes["k8s_version"]
	if !ver.IsOptional() || !ver.IsComputed() {
		t.Fatal("k8s_version must be Optional+Computed")
	}
}

// Follow-up 6: while the pool is not `active` (end of Create, upgrade
// running) its versions are provisional: a known planned value is kept.
func TestSetState_ProvisionalVersionNotActive(t *testing.T) {
	m := &poolResourceModel{K8sVersion: types.StringValue("v1.34.8")}
	setState(context.Background(), m, &client.K8sNodePool{ID: "id", ClusterID: "cid", Name: "n", Plan: "small",
		Replicas: 1, K8sVersion: strp("1.34.2"), Status: "creating"})
	if !m.K8sVersion.Equal(types.StringValue("v1.34.8")) {
		t.Fatalf("not active: k8s_version = %s, want the planned value", m.K8sVersion)
	}
	m = &poolResourceModel{K8sVersion: types.StringUnknown()}
	setState(context.Background(), m, &client.K8sNodePool{ID: "id", ClusterID: "cid", Name: "n", Plan: "small",
		Replicas: 1, K8sVersion: strp("1.34.8"), Status: "creating"})
	if !m.K8sVersion.Equal(types.StringValue("1.34.8")) {
		t.Fatalf("unknown planned value: must be known, got %s", m.K8sVersion)
	}
	m = &poolResourceModel{K8sVersion: types.StringValue("v1.34.8")}
	setState(context.Background(), m, &client.K8sNodePool{ID: "id", ClusterID: "cid", Name: "n", Plan: "small",
		Replicas: 1, K8sVersion: strp("1.34.2"), Status: "active"})
	if !m.K8sVersion.Equal(types.StringValue("1.34.2")) {
		t.Fatalf("active below: the actual version wins, got %s", m.K8sVersion)
	}
}
