// Package snatvalidator centralises the plan-time notice raised when a
// resource carries a cloud-init `user_data` script in an ISOLATED subnet —
// one whose `snat` is `false`, with no outbound internet access.
//
// **It warns; it does not refuse.** That is a deliberate change (#67), and the
// reasons are worth stating because the previous behaviour looked safer than
// it was:
//
//  1. An isolated subnet is a supported configuration, not a mistake. Since the
//     platform preloads images and serves name resolution from inside the
//     network, whole clusters live there. A `user_data` that writes a file,
//     creates a user or starts software already in the image needs no internet
//     at all — and the check never looks at the script, only at its presence.
//     A hard error therefore closed valid configurations, with no escape hatch.
//  2. The check FAILED OPEN. The lookup below returns silently on any probe
//     error, so a hard verdict was raised only when the probe worked, and
//     skipped when it did not — it blocked whoever it could reach and let
//     everyone else through. A gate that is bypassed by breaking its own probe
//     is not protecting anything; a warning that is occasionally absent is
//     honest about what it is.
//  3. The real failure, when it happens, occurs at first boot and is visible in
//     the guest's own logs. The warning brings that forward without deciding
//     for the operator.
//
// Each compute resource invokes `CheckVnetSnat` from its `ModifyPlan`, passing
// the `vnet_id`, a sentence describing what the script would lose, and the
// diagnostics collector.
package snatvalidator

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// CheckVnetSnat resolves the VNet by ID (no parent `vpc_id` required — see
// `client.FindVNetByID`) and adds a WARNING to `diags` when the subnet is
// isolated. Skips silently when `vnetID` is empty or the lookup itself fails.
func CheckVnetSnat(ctx context.Context, c *client.Client, vnetID, reason string, diags *diag.Diagnostics) {
	if c == nil || vnetID == "" {
		return
	}
	vnet, err := c.FindVNetByID(ctx, vnetID)
	if err != nil {
		// A probe failure says nothing about the subnet. Warning on it would
		// cry wolf on every network blip.
		return
	}
	if vnet == nil {
		return
	}
	if !vnet.SNAT {
		diags.AddWarning(
			"Startup script in an isolated subnet",
			fmt.Sprintf(
				"The subnet %q (id %s) is isolated: `snat = false`, so nothing in it reaches "+
					"the internet. %s\n\n"+
					"Whatever the script downloads will simply not resolve, and the resource "+
					"will finish starting without it — the failure shows up in its own boot "+
					"logs, not here. A script that only writes files, creates users or starts "+
					"software already present in the image runs exactly as it would elsewhere, "+
					"and this notice does not apply to it.\n\n"+
					"To give the subnet internet access, set `snat = true` on the `ccp_vnet`.",
				vnet.Name, vnet.ID, reason,
			),
		)
	}
}
