// Package snatvalidator centralises the plan-time check that a target
// VNet has outbound internet egress (`snat=true`) when the consumer
// resource depends on it (e.g. cloud-init `user_data` scripts that fetch
// packages, K8s/DB workloads that pull images at bootstrap).
//
// Each compute / managed resource invokes `CheckVnetSnat` from its
// `ModifyPlan` method, supplying the `vnet_id`, a reason string describing
// why internet egress matters for that resource, and the diagnostics
// collector. The check is a no-op when the lookup fails (network blip,
// transient API error) — failing the plan on a probe error would be
// worse UX than letting the apply surface the real error.
package snatvalidator

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// CheckVnetSnat resolves the VNet by ID (no parent `vpc_id` required —
// see `client.FindVNetByID`) and adds a hard error to `diags` if
// `snat=false`. Skips silently when `vnetID` is empty or the lookup
// itself fails.
func CheckVnetSnat(ctx context.Context, c *client.Client, vnetID, reason string, diags *diag.Diagnostics) {
	if c == nil || vnetID == "" {
		return
	}
	vnet, err := c.FindVNetByID(ctx, vnetID)
	if err != nil {
		// Don't fail the plan on a transient lookup error — apply will
		// surface a clearer error if the vnet really doesn't exist.
		return
	}
	if vnet == nil {
		return
	}
	if !vnet.SNAT {
		diags.AddError(
			"Target VNet has no outbound internet access",
			fmt.Sprintf(
				"The selected VNet %q (id %s) has `snat = false` — no outbound "+
					"NAT gateway is wired up. %s "+
					"Either set `snat = true` on the VNet or remove the "+
					"conflicting setting on this resource.",
				vnet.Name, vnet.ID, reason,
			),
		)
	}
}
