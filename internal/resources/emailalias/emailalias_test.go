// Tests for ccp_email_alias — the catch-all guard.
//
// `*@domain` with `wildcard = false` is a contradiction the platform and the
// mail server would read differently: the operator would believe the catch-all
// is off while it is on. The guard runs the real ValidateConfig against a real
// configuration value, not a reading of the schema.
package emailalias

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// validate runs ValidateConfig on a configuration built from the compiled
// schema. `wildcard` is passed as a tftypes.Value so a test can express the
// three states that matter: true, false, and not yet resolved.
func validate(t *testing.T, address string, wildcard tftypes.Value) *resource.ValidateConfigResponse {
	t.Helper()
	ctx := context.Background()
	r := &emailAliasResource{}

	var sresp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sresp)
	if sresp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sresp.Diagnostics)
	}

	objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"id":      tftypes.NewValue(tftypes.String, nil),
		"address": tftypes.NewValue(tftypes.String, address),
		"destinations": tftypes.NewValue(
			tftypes.List{ElementType: tftypes.String},
			[]tftypes.Value{tftypes.NewValue(tftypes.String, "contact@example.com")},
		),
		"wildcard":   wildcard,
		"comment":    tftypes.NewValue(tftypes.String, nil),
		"created_at": tftypes.NewValue(tftypes.String, nil),
	})

	resp := &resource.ValidateConfigResponse{}
	r.ValidateConfig(ctx, resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: raw, Schema: sresp.Schema},
	}, resp)
	return resp
}

func boolValue(b bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, b) }

func TestCatchAllWithoutTheOptionIsRejected(t *testing.T) {
	resp := validate(t, "*@example.com", boolValue(false))
	if !resp.Diagnostics.HasError() {
		t.Fatal("*@example.com with wildcard = false must be rejected at plan time")
	}
}

func TestCatchAllWithTheOptionIsAccepted(t *testing.T) {
	if resp := validate(t, "*@example.com", boolValue(true)); resp.Diagnostics.HasError() {
		t.Fatalf("a declared catch-all must be accepted: %v", resp.Diagnostics)
	}
}

func TestPreciseAddressIsUnaffected(t *testing.T) {
	if resp := validate(t, "team@example.com", boolValue(false)); resp.Diagnostics.HasError() {
		t.Fatalf("a precise address must not be touched by the guard: %v", resp.Diagnostics)
	}
}

// At `terraform validate` the default has not been applied yet and `wildcard`
// is UNKNOWN, not false. Treating unknown as a concrete false would raise the
// error on a configuration that is perfectly fine — CLAUDE.md pitfall #4, which
// once broke every consumer's `make validate`.
func TestUnknownWildcardDoesNotFireTheGuard(t *testing.T) {
	unknown := tftypes.NewValue(tftypes.Bool, tftypes.UnknownValue)
	if resp := validate(t, "*@example.com", unknown); resp.Diagnostics.HasError() {
		t.Fatalf("an unresolved wildcard must not fail validate: %v", resp.Diagnostics)
	}
	null := tftypes.NewValue(tftypes.Bool, nil)
	if resp := validate(t, "*@example.com", null); resp.Diagnostics.HasError() {
		t.Fatalf("a null wildcard must not fail validate: %v", resp.Diagnostics)
	}
}

func TestAddressPattern(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"contact@example.com", true},
		{"*@example.com", true},
		{"contact", false},
		{"contact@example", false},
		{"con tact@example.com", false},
		// Legal but exotic local parts must pass: forwarding destinations very
		// often point outside the platform, and refusing an address the
		// platform accepts would leave the operator with no way to tell which
		// of the two is wrong.
		{"a+b@example.com", true},
		{"first.last@sub.example.co.uk", true},
		{"x!y@example.com", true},
	} {
		if got := addressPattern.MatchString(tc.value); got != tc.valid {
			t.Errorf("address %q accepted=%v, want %v", tc.value, got, tc.valid)
		}
	}
}
