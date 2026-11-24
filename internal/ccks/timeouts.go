package ccks

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TimeoutsAttribute is an optional `timeouts` object of Go durations
// (`30m`, `1h30m`), one attribute per operation, documented with its default.
// Optional only (never computed, never sent to the API): the state is the
// configuration, so adding or removing it plans an update without any call.
func TimeoutsAttribute(defaults map[string]string) schema.SingleNestedAttribute {
	attrs := map[string]schema.Attribute{}
	for name, def := range defaults {
		attrs[name] = schema.StringAttribute{
			MarkdownDescription: fmt.Sprintf("Go duration (e.g. `30m`, `1h30m`). %s", def),
			Optional:            true,
			Validators:          []validator.String{durationValidator{}},
		}
	}
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Waiting times of the provider. Unset: the defaults below.",
		Optional:            true,
		Attributes:          attrs,
	}
}

// TimeoutsAttrTypes is the object type of a TimeoutsAttribute with names.
func TimeoutsAttrTypes(names ...string) map[string]attr.Type {
	out := map[string]attr.Type{}
	for _, n := range names {
		out[n] = types.StringType
	}
	return out
}

// Timeout reads one duration of a `timeouts` object, def when unset.
func Timeout(o types.Object, name string, def time.Duration) time.Duration {
	if o.IsNull() || o.IsUnknown() {
		return def
	}
	v, ok := o.Attributes()[name].(types.String)
	if !ok || v.IsNull() || v.IsUnknown() {
		return def
	}
	d, err := time.ParseDuration(v.ValueString())
	if err != nil || d <= 0 {
		return def
	}
	return d
}

type durationValidator struct{}

func (durationValidator) Description(context.Context) string { return "a positive Go duration" }
func (v durationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (durationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if d, err := time.ParseDuration(req.ConfigValue.ValueString()); err != nil || d <= 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid duration",
			fmt.Sprintf("%q is not a positive Go duration (e.g. 30m, 1h30m).", req.ConfigValue.ValueString()))
	}
}

// VersionRegex is the format of a Kubernetes version the API accepts.
const VersionRegex = `^[vV]?\d+\.\d+\.\d+$`
