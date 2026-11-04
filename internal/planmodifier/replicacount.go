package planmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// legacyReplicaCount is the instance count of every gateway created before
// `replica_count` existed: they all run in high availability.
const legacyReplicaCount = 2

// ReplicaCountRequiresReplace forces replacement when `replica_count` changes,
// except when the prior state predates the attribute (null) and the plan asks
// for the historical value (2). Such a state is written by an older provider;
// with `-refresh=false` (or a plan computed before the first refresh) plain
// RequiresReplace would compare null to the default 2 and plan the
// destruction of every existing Application Gateway and Load Balancer.
func ReplicaCountRequiresReplace() planmodifier.Int64 {
	return int64planmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
			if req.StateValue.IsNull() {
				resp.RequiresReplace = !req.PlanValue.Equal(types.Int64Value(legacyReplicaCount))
				return
			}
			resp.RequiresReplace = true
		},
		"Changing the instance count replaces the resource; a state written before the attribute existed counts as 2.",
		"Changing the instance count replaces the resource; a state written before the attribute existed counts as `2`.",
	)
}
