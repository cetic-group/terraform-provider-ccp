package planmodifier

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type legacyStringDefault struct{ def string }

// LegacyStringDefault gives an Optional+Computed string attribute a default
// without turning a state written before the attribute existed into a diff.
//
// When the attribute is not configured, the plan is the default on creation and
// whenever the prior state holds a value. A prior state where the attribute is
// null (written by an older provider, planned with -refresh=false or before the
// first refresh) is left untouched (null, or unknown if the resource changes
// for another reason), so an existing resource shows no change.
func LegacyStringDefault(def string) planmodifier.String { return legacyStringDefault{def: def} }

func (m legacyStringDefault) Description(context.Context) string {
	return "Defaults to " + m.def + "; a state written before the attribute existed is left unchanged."
}

func (m legacyStringDefault) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m legacyStringDefault) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	if !req.State.Raw.IsNull() && req.StateValue.IsNull() {
		// State written before the attribute existed. Without another change the
		// plan equals the state (null): keep it, so nothing is shown. When the
		// resource is modified for another reason the framework marked this
		// attribute unknown and Update will fill it: keep it unknown, a null
		// plan would make the applied value inconsistent with the plan.
		return
	}
	resp.PlanValue = types.StringValue(m.def)
}

// RequiresReplaceUnlessLegacy forces replacement when the attribute changes,
// except when the prior state predates the attribute (null) and the plan asks
// for the historical value `legacy`.
func RequiresReplaceUnlessLegacy(legacy string) planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			if req.StateValue.IsNull() {
				// An unknown plan is a legacy state modified for another reason.
				resp.RequiresReplace = !req.PlanValue.IsUnknown() && !req.PlanValue.Equal(types.StringValue(legacy))
				return
			}
			resp.RequiresReplace = true
		},
		"Changing it replaces the resource; a state written before the attribute existed counts as "+legacy+".",
		"Changing it replaces the resource; a state written before the attribute existed counts as `"+legacy+"`.",
	)
}
