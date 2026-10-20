package schedule

import (
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strp(s string) *string { return &s }

// A schedule ended by its period is paused by the platform; state keeps the
// configured value so the plan does not show a diff that cannot be applied.
func TestEffectiveEnabled(t *testing.T) {
	ended := &client.Schedule{Enabled: false, EndedAt: strp("2027-02-20T00:01:00Z")}
	if got := effectiveEnabled(ended, types.BoolValue(true)); !got.ValueBool() {
		t.Fatal("ended schedule must keep the configured enabled=true")
	}
	if got := effectiveEnabled(ended, types.BoolNull()); got.ValueBool() {
		t.Fatal("import (no prior value) must report the real value")
	}
	paused := &client.Schedule{Enabled: false}
	if got := effectiveEnabled(paused, types.BoolValue(true)); got.ValueBool() {
		t.Fatal("a schedule paused by hand is real drift and must be reported")
	}
	running := &client.Schedule{Enabled: true}
	if got := effectiveEnabled(running, types.BoolValue(false)); !got.ValueBool() {
		t.Fatal("server value wins when the schedule has not ended")
	}
}

func TestDateField(t *testing.T) {
	if got, _ := dateField(types.StringNull()).MarshalJSON(); string(got) != "null" {
		t.Fatalf("removed bound must clear: %s", got)
	}
	f := dateField(types.StringValue("2027-02-19"))
	got, _ := f.MarshalJSON()
	if !f.IsSet() || string(got) != `"2027-02-19"` {
		t.Fatalf("bound not set: %s", got)
	}
}
