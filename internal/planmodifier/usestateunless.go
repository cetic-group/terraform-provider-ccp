package planmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type useStateUnlessChanged struct{ watched []path.Path }

// UseStateUnlessChanged keeps the prior state value of a computed string in the
// plan, like UseStateForUnknown, EXCEPT when one of the watched (string) attributes
// changes: the platform then sets the value during apply, so it must stay
// unknown ("known after apply").
//
// Plain UseStateForUnknown freezes the old value in the plan, and the apply
// fails with "Provider produced inconsistent result" once the platform returns
// the new one (terraform-provider-ccp#117: `public_ip_address` when
// `public_ip_id` is swapped).
func UseStateUnlessChanged(watched ...path.Path) planmodifier.String {
	return useStateUnlessChanged{watched: watched}
}

func (m useStateUnlessChanged) Description(context.Context) string {
	return "Keeps the prior state value unless a related attribute changes."
}

func (m useStateUnlessChanged) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m useStateUnlessChanged) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Creation (no prior state) or destruction (no plan): nothing to keep.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	// A value already known in the plan is left alone.
	if !req.PlanValue.IsUnknown() {
		return
	}
	for _, p := range m.watched {
		var planned, prior types.String
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, p, &planned)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, p, &prior)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if planned.IsUnknown() || !planned.Equal(prior) {
			return
		}
	}
	resp.PlanValue = req.StateValue
}
