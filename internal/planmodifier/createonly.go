package planmodifier

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// CreateOnlyString keeps the prior state value of an attribute that is only
// applied at creation and that the platform may change afterwards (image tag
// raised by the back-office, …). A configured value that differs from the
// state produces a warning instead of a replacement: replacing a registry
// would destroy its images (cetic-cloud-platform#2004, review of
// terraform-provider-ccp#121).
func CreateOnlyString(what string) planmodifier.String { return createOnlyString{what: what} }

type createOnlyString struct{ what string }

func (m createOnlyString) Description(context.Context) string {
	return "Applied at creation only; later differences are reported, never replaced."
}

func (m createOnlyString) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (m createOnlyString) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return // creation: the configured value is sent
	}
	if !req.ConfigValue.IsNull() && !req.ConfigValue.IsUnknown() && !req.ConfigValue.Equal(req.StateValue) {
		resp.Diagnostics.AddAttributeWarning(req.Path, fmt.Sprintf("%s is applied at creation only", m.what),
			fmt.Sprintf("The platform reports %q, the configuration says %q. The value is not changed "+
				"on an existing resource and does not force a replacement; align the configuration "+
				"to silence this warning.", req.StateValue.ValueString(), req.ConfigValue.ValueString()))
	}
	resp.PlanValue = req.StateValue
}

// CreateOnlyInt64 is CreateOnlyString for an integer (firewall rule
// `position`, renumbered by the console when rules are reordered).
func CreateOnlyInt64(what string) planmodifier.Int64 { return createOnlyInt64{what: what} }

type createOnlyInt64 struct{ what string }

func (m createOnlyInt64) Description(context.Context) string {
	return "Applied at creation only; later differences are reported, never replaced."
}

func (m createOnlyInt64) MarkdownDescription(ctx context.Context) string { return m.Description(ctx) }

func (m createOnlyInt64) PlanModifyInt64(_ context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if req.State.Raw.IsNull() || req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if !req.ConfigValue.IsNull() && !req.ConfigValue.IsUnknown() && !req.ConfigValue.Equal(req.StateValue) {
		resp.Diagnostics.AddAttributeWarning(req.Path, fmt.Sprintf("%s is applied at creation only", m.what),
			fmt.Sprintf("The platform reports %d, the configuration says %d. The value is not changed "+
				"on an existing resource and does not force a replacement.",
				req.StateValue.ValueInt64(), req.ConfigValue.ValueInt64()))
	}
	resp.PlanValue = req.StateValue
}

// TimeWithinTolerance keeps the state value of an RFC 3339 timestamp when the
// configured value designates the same instant within `tolerance`. It must run
// BEFORE RequiresReplace: the API stores whole days (`expires_in_days`), so the
// state of a service account written by an older provider holds the exact API
// time, and without this modifier the first plan after an upgrade would
// replace the account (review of terraform-provider-ccp#121).
func TimeWithinTolerance(tolerance time.Duration) planmodifier.String {
	return timeWithinTolerance{tolerance: tolerance}
}

type timeWithinTolerance struct{ tolerance time.Duration }

func (m timeWithinTolerance) Description(context.Context) string {
	return fmt.Sprintf("Ignores differences smaller than %s.", m.tolerance)
}

func (m timeWithinTolerance) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m timeWithinTolerance) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if SameInstant(req.ConfigValue, req.StateValue, m.tolerance) {
		resp.PlanValue = req.StateValue
	}
}

// SameInstant reports whether two known RFC 3339 values are less than
// `tolerance` apart.
func SameInstant(a, b types.String, tolerance time.Duration) bool {
	if a.IsNull() || a.IsUnknown() || b.IsNull() || b.IsUnknown() {
		return false
	}
	ta, err1 := time.Parse(time.RFC3339, a.ValueString())
	tb, err2 := time.Parse(time.RFC3339, b.ValueString())
	if err1 != nil || err2 != nil {
		return false
	}
	d := ta.Sub(tb)
	return d < tolerance && d > -tolerance
}
