package dbpginstance

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestProvisionTimeoutScalesWithReplicas pins the behaviour that a 3-replica HA
// cluster gets more than the 10 flat minutes that used to time out on a
// provisioning still making progress (measured at 14m40s to service_ready).
func TestProvisionTimeoutScalesWithReplicas(t *testing.T) {
	cases := []struct {
		name     string
		replicas types.Int64
		want     time.Duration
	}{
		{"null vaut une instance seule", types.Int64Null(), 15 * time.Minute},
		{"unknown vaut une instance seule", types.Int64Unknown(), 15 * time.Minute},
		{"instance seule", types.Int64Value(1), 15 * time.Minute},
		{"cluster HA a trois replicas", types.Int64Value(3), 35 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := provisionTimeout(c.replicas); got != c.want {
				t.Errorf("provisionTimeout(%v) = %v, attendu %v", c.replicas, got, c.want)
			}
		})
	}

	// Le defaut ne doit jamais redescendre sous la duree mesuree en production.
	if provisionTimeout(types.Int64Value(3)) <= 14*time.Minute+40*time.Second {
		t.Error("le delai HA doit depasser les 14m40s observes jusqu'a service_ready")
	}
}
