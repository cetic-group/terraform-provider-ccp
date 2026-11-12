package planmodifier

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func rawState(exists bool) tfsdk.State {
	if !exists {
		return tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, nil)}
	}
	return tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, map[string]tftypes.Value{})}
}

func runLegacy(config, state, plan types.String, exists bool) types.String {
	req := planmodifier.StringRequest{ConfigValue: config, StateValue: state, PlanValue: plan, State: rawState(exists)}
	resp := &planmodifier.StringResponse{PlanValue: plan}
	LegacyStringDefault("upstream").PlanModifyString(context.Background(), req, resp)
	return resp.PlanValue
}

func TestLegacyStringDefault(t *testing.T) {
	null, unknown := types.StringNull(), types.StringUnknown()
	cases := []struct {
		name                string
		config, state, plan types.String
		exists              bool
		want                types.String
	}{
		{"creation", null, null, unknown, false, types.StringValue("upstream")},
		{"configured value untouched", types.StringValue("bucket"), null, types.StringValue("bucket"), true, types.StringValue("bucket")},
		{"existing state with value", null, types.StringValue("bucket"), types.StringValue("bucket"), true, types.StringValue("upstream")},
		{"legacy state, no other change", null, null, null, true, null},
		{"legacy state, other change keeps unknown", null, null, unknown, true, unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runLegacy(tc.config, tc.state, tc.plan, tc.exists); !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRequiresReplaceUnlessLegacy(t *testing.T) {
	cases := []struct {
		name        string
		state, plan types.String
		want        bool
	}{
		{"legacy to upstream", types.StringNull(), types.StringValue("upstream"), false},
		{"legacy to bucket", types.StringNull(), types.StringValue("bucket"), true},
		{"legacy modified elsewhere (unknown)", types.StringNull(), types.StringUnknown(), false},
		{"upstream to bucket", types.StringValue("upstream"), types.StringValue("bucket"), true},
		{"bucket to upstream", types.StringValue("bucket"), types.StringValue("upstream"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := RequiresReplaceUnlessLegacy("upstream")
			req := planmodifier.StringRequest{StateValue: tc.state, PlanValue: tc.plan, ConfigValue: tc.plan,
				State: rawState(true), Plan: tfsdk.Plan{Raw: tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, map[string]tftypes.Value{})}}
			resp := &planmodifier.StringResponse{PlanValue: tc.plan}
			mod.PlanModifyString(context.Background(), req, resp)
			if resp.RequiresReplace != tc.want {
				t.Fatalf("RequiresReplace=%v, want %v", resp.RequiresReplace, tc.want)
			}
		})
	}
}
