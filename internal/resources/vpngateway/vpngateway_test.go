package vpngateway

import (
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

// Régression : le Create de la gateway ne pollait pas jusqu'à `active` et
// rendait la main en `provisioning`. Conséquences : les peers (ccp_vpn_peer)
// partaient trop tôt → 409 « La gateway VPN n'est pas encore prête », et les
// outputs endpoint_host/public_key restaient nuls après le 1er apply. Le Create
// poll désormais via classifyGatewayProvision. Cf. incident 2026-06-14.
func TestClassifyGatewayProvision(t *testing.T) {
	cases := []struct {
		name     string
		status   string
		wantDone bool
		wantErr  bool
	}{
		{"active", client.VPNGatewayStatusActive, true, false},
		{"provisioning", client.VPNGatewayStatusProvisioning, false, false}, // transitoire → poll
		{"error", client.VPNGatewayStatusError, false, true},                // échec dur
		{"deleting", client.VPNGatewayStatusDeleting, false, false},         // pas terminal côté create → poll
		{"unknown", "banana", false, false},                                 // statut inattendu → on continue de poller (timeout tranchera)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done, err := classifyGatewayProvision("gw-1", c.status)
			if done != c.wantDone {
				t.Errorf("status %q: done = %v, want %v", c.status, done, c.wantDone)
			}
			if (err != nil) != c.wantErr {
				t.Errorf("status %q: err = %v, wantErr = %v", c.status, err, c.wantErr)
			}
		})
	}
}
