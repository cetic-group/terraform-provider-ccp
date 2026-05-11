// Package planmodifier hosts cross-resource PlanModifiers.
//
// JSONNormalizeEqual is the v0.11.0 addition: it suppresses spurious diffs
// when the user-provided JSON differs from the API-canonicalised JSON only
// in key ordering / whitespace. Used by `ccp_iam_role.policy_document_json`
// where the API re-serializes via JCS RFC 8785-equivalent canonicalisation
// and may return keys in a different order than the user wrote them.
//
// Algorithm: when state is non-null and plan is non-null, parse both as
// generic JSON and compare semantically. If they are semantically equal,
// force the planned value to the existing state value to keep diff stable.
package planmodifier

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// JSONNormalizeEqual returns a planmodifier.String that suppresses diffs
// for JSON-valued string attributes when state/plan differ only by
// formatting (key order, whitespace, etc.).
func JSONNormalizeEqual() planmodifier.String { return &jsonNormalizeEqualModifier{} }

type jsonNormalizeEqualModifier struct{}

func (m *jsonNormalizeEqualModifier) Description(_ context.Context) string {
	return "Suppresses diffs for JSON-valued attributes when state and plan are " +
		"semantically equivalent (same parsed structure)."
}

func (m *jsonNormalizeEqualModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m *jsonNormalizeEqualModifier) PlanModifyString(
	ctx context.Context,
	req planmodifier.StringRequest,
	resp *planmodifier.StringResponse,
) {
	// Bail-out cases: no diff to suppress.
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		// Config was set to null on this run — let terraform compute the
		// natural diff; don't override.
		return
	}

	stateJSON := req.StateValue.ValueString()
	planJSON := req.PlanValue.ValueString()
	if stateJSON == planJSON {
		return // identical bytes already
	}

	var stateAny, planAny any
	if err := json.Unmarshal([]byte(stateJSON), &stateAny); err != nil {
		return // malformed state — let terraform surface the diff
	}
	if err := json.Unmarshal([]byte(planJSON), &planAny); err != nil {
		return // malformed plan — let terraform surface the diff
	}

	if reflect.DeepEqual(stateAny, planAny) {
		// Semantically equal → preserve state value to avoid spurious diff.
		resp.PlanValue = req.StateValue
	}
}
