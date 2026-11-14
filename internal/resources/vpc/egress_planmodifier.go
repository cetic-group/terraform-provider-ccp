package vpc

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// useStateUnlessEgressChanges keeps the state value of an `egress_*` attribute
// in the plan, like UseStateForUnknown, EXCEPT when `dedicated_egress_ip`
// changes: the IP, its type and its status are then decided by the platform
// during apply, so they must stay unknown ("known after apply").
//
// Plain UseStateForUnknown would be wrong here: on `false` → `true` it would
// plan a null `egress_ip` and the apply would fail with "Provider produced
// inconsistent result" once the platform returns the IP; on `true` → `false`
// it would freeze the released IP in the plan. Without any modifier, every
// in-place update would show the IP as "known after apply" even when the
// option does not change. Today `dedicated_egress_ip` is the only attribute
// updated in place, so both behaviours coincide; this one stays right if
// another in-place attribute is added.
func useStateUnlessEgressChanges() planmodifier.String {
	return egressStateModifier{}
}

type egressStateModifier struct{}

func (egressStateModifier) Description(_ context.Context) string {
	return "Keeps the prior state value unless `dedicated_egress_ip` changes."
}

func (m egressStateModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (egressStateModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Creation (no prior state) or destruction (no plan): nothing to keep.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	// A value already known in the plan is left alone.
	if !req.PlanValue.IsUnknown() {
		return
	}

	var planned, prior types.Bool
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("dedicated_egress_ip"), &planned)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("dedicated_egress_ip"), &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if planned.IsUnknown() || !planned.Equal(prior) {
		return
	}
	resp.PlanValue = req.StateValue
}

// alwaysNull plans `null` for an attribute the provider never fills any more
// (`egress_ip_type`, cetic-cloud-platform#1977). Keeping the state value
// instead would plan a stale kind that the apply then writes as `null`:
// "Provider produced inconsistent result" (e.g. `apply -refresh=false`).
func alwaysNull() planmodifier.String {
	return alwaysNullModifier{}
}

type alwaysNullModifier struct{}

func (alwaysNullModifier) Description(_ context.Context) string {
	return "Always plans null (attribute no longer reported by the platform)."
}

func (m alwaysNullModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (alwaysNullModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.Plan.Raw.IsNull() { // destruction
		return
	}
	resp.PlanValue = types.StringNull()
}
