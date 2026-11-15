package planmodifier

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Schéma minimal : `addr` (calculé) suit `ref` (Optional+Computed, gardé de
// l'état quand il n'est pas écrit) et `size` (entier, Optional).
var unlessSchema = schema.Schema{Attributes: map[string]schema.Attribute{
	"ref":  schema.StringAttribute{Optional: true, Computed: true},
	"size": schema.Int64Attribute{Optional: true},
	"addr": schema.StringAttribute{Computed: true},
}}

var unlessType = unlessSchema.Type().TerraformType(context.Background())

const unk = "<unknown>"

func unlessVal(ref, size, addr any) tftypes.Value {
	v := func(t tftypes.Type, x any) tftypes.Value {
		if x == unk {
			return tftypes.NewValue(t, tftypes.UnknownValue)
		}
		return tftypes.NewValue(t, x)
	}
	return tftypes.NewValue(unlessType, map[string]tftypes.Value{
		"ref": v(tftypes.String, ref), "size": v(tftypes.Number, size), "addr": v(tftypes.String, addr),
	})
}

// run returns the planned `addr`. A nil state is a creation, a nil plan a destroy.
func runUnless(t *testing.T, config, plan, state *tftypes.Value) types.String {
	t.Helper()
	raw := func(v *tftypes.Value) tftypes.Value {
		if v == nil {
			return tftypes.NewValue(unlessType, nil)
		}
		return *v
	}
	req := planmodifier.StringRequest{
		Path:   path.Root("addr"),
		Config: tfsdk.Config{Schema: unlessSchema, Raw: raw(config)},
		Plan:   tfsdk.Plan{Schema: unlessSchema, Raw: raw(plan)},
		State:  tfsdk.State{Schema: unlessSchema, Raw: raw(state)},
	}
	req.PlanValue = types.StringUnknown()
	if plan != nil {
		var pv types.String
		_ = req.Plan.GetAttribute(context.Background(), path.Root("addr"), &pv)
		req.PlanValue = pv
	}
	if state != nil {
		var sv types.String
		_ = req.State.GetAttribute(context.Background(), path.Root("addr"), &sv)
		req.StateValue = sv
	} else {
		req.StateValue = types.StringNull()
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	UseStateUnlessChanged(path.Root("ref"), path.Root("size")).PlanModifyString(context.Background(), req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	return resp.PlanValue
}

func ptr(v tftypes.Value) *tftypes.Value { return &v }

func TestUseStateUnlessChanged(t *testing.T) {
	state := ptr(unlessVal("ip-1", 10, "203.0.113.7"))
	cases := []struct {
		name          string
		config, plan  *tftypes.Value
		state         *tftypes.Value
		wantKnown     bool
		wantStateAddr bool
	}{
		{"rien de surveillé ne change : valeur de l'état",
			ptr(unlessVal("ip-1", 10, nil)), ptr(unlessVal("ip-1", 10, unk)), state, true, true},
		{"référence changée dans la config : inconnue",
			ptr(unlessVal("ip-2", 10, nil)), ptr(unlessVal("ip-2", 10, unk)), state, false, false},
		{"entier surveillé changé : inconnue",
			ptr(unlessVal("ip-1", 20, nil)), ptr(unlessVal("ip-1", 20, unk)), state, false, false},
		{"entier retiré de la config (Optional seul, null) : inconnue",
			ptr(unlessVal("ip-1", nil, nil)), ptr(unlessVal("ip-1", nil, unk)), state, false, false},
		{"référence non écrite, encore inconnue au plan : gardée de l'état",
			ptr(unlessVal(nil, 10, nil)), ptr(unlessVal(unk, 10, unk)), state, true, true},
		{"référence écrite mais inconnue (valeur d'une autre ressource) : inconnue",
			ptr(unlessVal(unk, 10, nil)), ptr(unlessVal(unk, 10, unk)), state, false, false},
		{"création : inconnue",
			ptr(unlessVal("ip-1", 10, nil)), ptr(unlessVal("ip-1", 10, unk)), nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runUnless(t, c.config, c.plan, c.state)
			if got.IsUnknown() == c.wantKnown {
				t.Fatalf("addr planifiée %v, connue attendue=%v", got, c.wantKnown)
			}
			if c.wantStateAddr && got.ValueString() != "203.0.113.7" {
				t.Fatalf("addr planifiée %v, valeur de l'état attendue", got)
			}
		})
	}
}

func TestUseStateUnlessChanged_KnownPlanAndDestroyUntouched(t *testing.T) {
	state := ptr(unlessVal("ip-1", 10, "203.0.113.7"))
	if got := runUnless(t, ptr(unlessVal("ip-2", 10, nil)), ptr(unlessVal("ip-2", 10, "198.51.100.1")), state); got.ValueString() != "198.51.100.1" {
		t.Fatalf("une valeur déjà connue au plan doit rester, reçu %v", got)
	}
	if got := runUnless(t, nil, nil, state); !got.IsUnknown() {
		t.Fatalf("destruction : rien à garder, reçu %v", got)
	}
}
