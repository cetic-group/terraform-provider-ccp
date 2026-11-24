package k8scluster

// Plan of the control plane (cetic-cloud-platform#2143) and its deprecated
// alias `tier`.
//
// `plan` (starter | standard | pro) and `tier` (dev | prod) are both
// Optional+Computed and always consistent: tier = dev ⟺ plan = starter. The
// effective plan is computed by ONE function from the configuration and the
// prior state only, so the plan modifiers of the two attributes never depend
// on each other's planned value (their order is not guaranteed):
//
//   - `plan` configured → that plan;
//   - `tier` configured only → the prior plan when it has that tier (a `pro`
//     cluster configured `tier = "prod"` stays `pro`), else the tier's plan
//     (`dev` → starter, `prod` → standard);
//   - neither → the prior plan, `starter` at creation.
//
// A configuration written before #2143 (`tier = "prod"` or no tier) thus
// plans no change on an existing cluster. Standard ↔ Pro is changed in place
// (PATCH); Starter ↔ a paid plan changes the apiserver frontend topology,
// which the platform refuses in place (409): it forces a replacement.

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	planStarter  = "starter"
	planStandard = "standard"
	planPro      = "pro"
	tierDev      = "dev"
	tierProd     = "prod"
)

// tierOf is the deprecated tier equivalent of a plan.
func tierOf(plan string) string {
	if plan == planStarter {
		return tierDev
	}
	return tierProd
}

// planOfTier is the plan a deprecated tier stands for (as the API maps it).
func planOfTier(tier string) string {
	if tier == tierProd {
		return planStandard
	}
	return planStarter
}

// planOrTier is the plan of an API response: `plan`, or derived from `tier`
// on an API older than #2143.
func planOrTier(plan, tier string) string {
	if plan != "" {
		return plan
	}
	return planOfTier(tier)
}

type planInputs struct {
	cfgPlan, cfgTier     types.String
	statePlan, stateTier types.String
	stateExists          bool
}

func readPlanInputs(ctx context.Context, config tfsdk.Config, state tfsdk.State) (planInputs, error) {
	var in planInputs
	if d := config.GetAttribute(ctx, path.Root("plan"), &in.cfgPlan); d.HasError() {
		return in, fmt.Errorf("reading plan: %v", d)
	}
	if d := config.GetAttribute(ctx, path.Root("tier"), &in.cfgTier); d.HasError() {
		return in, fmt.Errorf("reading tier: %v", d)
	}
	if state.Raw.IsNull() {
		return in, nil
	}
	in.stateExists = true
	if d := state.GetAttribute(ctx, path.Root("plan"), &in.statePlan); d.HasError() {
		return in, fmt.Errorf("reading prior plan: %v", d)
	}
	if d := state.GetAttribute(ctx, path.Root("tier"), &in.stateTier); d.HasError() {
		return in, fmt.Errorf("reading prior tier: %v", d)
	}
	return in, nil
}

func set(v types.String) bool { return !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" }

// priorPlan is the plan of the prior state ("" at creation). A state written
// before #2143 has no plan: it is derived from its tier.
func (in planInputs) priorPlan() string {
	switch {
	case !in.stateExists:
		return ""
	case set(in.statePlan):
		return in.statePlan.ValueString()
	case set(in.stateTier):
		return planOfTier(in.stateTier.ValueString())
	}
	return ""
}

// effective is the planned plan; known is false when the configuration is
// not known yet (value from another resource).
func (in planInputs) effective() (plan string, known bool) {
	if in.cfgPlan.IsUnknown() || in.cfgTier.IsUnknown() {
		return "", false
	}
	if set(in.cfgPlan) {
		return in.cfgPlan.ValueString(), true
	}
	prior := in.priorPlan()
	if set(in.cfgTier) {
		if prior != "" && tierOf(prior) == in.cfgTier.ValueString() {
			return prior, true
		}
		return planOfTier(in.cfgTier.ValueString()), true
	}
	if prior != "" {
		return prior, true
	}
	return planStarter, true
}

// requiresReplace: Starter ↔ paid plan (frontend topology change).
func (in planInputs) requiresReplace() bool {
	prior := in.priorPlan()
	eff, known := in.effective()
	return known && prior != "" && (prior == planStarter) != (eff == planStarter)
}

// ── plan ─────────────────────────────────────────────────────────────────

type planAttrModifier struct{}

func (planAttrModifier) Description(context.Context) string {
	return "Derives the plan from `tier` (deprecated) or the prior state; Starter ↔ paid plan forces replacement."
}
func (m planAttrModifier) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (planAttrModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.Plan.Raw.IsNull() { // destroy
		return
	}
	in, err := readPlanInputs(ctx, req.Config, req.State)
	if err != nil {
		resp.Diagnostics.AddError("Cannot plan `plan`", err.Error())
		return
	}
	eff, known := in.effective()
	if !known {
		resp.PlanValue = types.StringUnknown()
		if in.priorPlan() != "" {
			// Cannot tell now whether it crosses Starter ↔ paid: never plan a
			// destruction on a guess; the apply refuses a crossing instead.
			resp.Diagnostics.AddAttributeWarning(req.Path, "Plan not known yet",
				"`plan` (or `tier`) depends on a value known only after apply: a change between "+
					"`starter` and a paid plan cannot be planned as a replacement. If it turns out to be one, "+
					"the apply stops with an error before changing anything; recreate the cluster with "+
					"`terraform apply -replace`.")
		}
		return
	}
	resp.PlanValue = types.StringValue(eff)
	resp.RequiresReplace = in.requiresReplace()
}

// ── tier (deprecated) ────────────────────────────────────────────────────

type tierAttrModifier struct{}

func (tierAttrModifier) Description(context.Context) string {
	return "Derives the deprecated tier from the effective plan."
}
func (m tierAttrModifier) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (tierAttrModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	in, err := readPlanInputs(ctx, req.Config, req.State)
	if err != nil {
		resp.Diagnostics.AddError("Cannot plan `tier`", err.Error())
		return
	}
	eff, known := in.effective()
	if !known {
		resp.PlanValue = types.StringUnknown()
		return
	}
	resp.PlanValue = types.StringValue(tierOf(eff))
}

// tierMatchesPlan rejects `plan` and `tier` both configured and inconsistent
// (the API answers 422).
type tierMatchesPlan struct{}

func (tierMatchesPlan) Description(context.Context) string {
	return "When `plan` is also set, `tier` must be its equivalent (`dev` for starter, `prod` otherwise)."
}
func (v tierMatchesPlan) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (tierMatchesPlan) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if !set(req.ConfigValue) {
		return
	}
	var cfgPlan types.String
	if d := req.Config.GetAttribute(ctx, path.Root("plan"), &cfgPlan); d.HasError() || !set(cfgPlan) {
		return
	}
	if tierOf(cfgPlan.ValueString()) != req.ConfigValue.ValueString() {
		resp.Diagnostics.AddAttributeError(req.Path, "`plan` and `tier` disagree",
			fmt.Sprintf("plan %q is tier %q, not %q. `tier` is deprecated: set `plan` only.",
				cfgPlan.ValueString(), tierOf(cfgPlan.ValueString()), req.ConfigValue.ValueString()))
	}
}

// ── values that follow the plan (etcd_volume_gb, node_ip_block_size) ─────

// stateUnlessPlanChanges keeps the prior value while the plan does not
// change; a plan change (more etcd volume, a larger IP block) leaves it known
// after apply.
type stateUnlessPlanChanges struct{}

func (stateUnlessPlanChanges) Description(context.Context) string {
	return "Keeps the prior value unless the plan changes."
}
func (m stateUnlessPlanChanges) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (stateUnlessPlanChanges) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.StateValue.IsNull() {
		return
	}
	in, err := readPlanInputs(ctx, req.Config, req.State)
	if err != nil {
		return
	}
	if eff, known := in.effective(); known && eff == in.priorPlan() {
		resp.PlanValue = req.StateValue
	}
}

// ── current_k8s_version ──────────────────────────────────────────────────

// stateUnlessVersionChanges keeps the prior actual version while the desired
// `k8s_version` is not changed.
type stateUnlessVersionChanges struct{}

func (stateUnlessVersionChanges) Description(context.Context) string {
	return "Keeps the prior value unless `k8s_version` changes."
}
func (m stateUnlessVersionChanges) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (stateUnlessVersionChanges) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || !req.PlanValue.IsUnknown() || req.StateValue.IsNull() {
		return
	}
	var cfg, prior types.String
	if d := req.Config.GetAttribute(ctx, path.Root("k8s_version"), &cfg); d.HasError() {
		return
	}
	if d := req.State.GetAttribute(ctx, path.Root("k8s_version"), &prior); d.HasError() {
		return
	}
	if cfg.Equal(prior) {
		resp.PlanValue = req.StateValue
	}
}
