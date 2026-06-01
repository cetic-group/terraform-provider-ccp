package k8snodepool

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

func ptr(i int) *int { return &i }

// setState doit normaliser l'état "autoscaler désactivé" (max_size 0/null) vers
// null/null, car min_size/max_size sont Optional (non-Computed) : le state final
// doit == la config (null). Sinon : "inconsistent result: was null, but now 0".
func TestSetState_AutoscalerNormalization(t *testing.T) {
	cases := []struct {
		name            string
		min, max        *int
		wantMinNull     bool
		wantMaxNull     bool
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
