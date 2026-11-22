package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestAppGWListenerDomainCacheReadOnlyPlan(t *testing.T) {
	ctx := context.Background()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	typ := sch.ResourceSchemas["ccp_appgw_listener"].ValueType().(tftypes.Object)
	enc := func(v tftypes.Value) *tfprotov6.DynamicValue {
		d, err := tfprotov6.NewDynamicValue(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	obj := func(set map[string]tftypes.Value) tftypes.Value {
		m := map[string]tftypes.Value{}
		for k, at := range typ.AttributeTypes {
			if v, ok := set[k]; ok {
				m[k] = v
			} else {
				m[k] = tftypes.NewValue(at, nil)
			}
		}
		return tftypes.NewValue(typ, m)
	}
	str := func(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
	base := map[string]tftypes.Value{
		"id": str("listener-1"), "appgw_id": str("gw-1"), "hostname": str("api.example.com"),
	}
	for _, tc := range []struct {
		name  string
		cache tftypes.Value
	}{
		{"historical null", tftypes.NewValue(tftypes.Bool, nil)},
		{"disabled", tftypes.NewValue(tftypes.Bool, false)},
		{"enabled", tftypes.NewValue(tftypes.Bool, true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			priorFields := map[string]tftypes.Value{}
			for k, v := range base {
				priorFields[k] = v
			}
			priorFields["cache_enabled"] = tc.cache
			prior := obj(priorFields)
			config := obj(map[string]tftypes.Value{
				"appgw_id": base["appgw_id"], "hostname": base["hostname"],
			})
			plan, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName: "ccp_appgw_listener", PriorState: enc(prior),
				ProposedNewState: enc(prior), Config: enc(config),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range plan.Diagnostics {
				if d.Severity == tfprotov6.DiagnosticSeverityError {
					t.Fatalf("plan: %s: %s", d.Summary, d.Detail)
				}
			}
			if len(plan.RequiresReplace) > 0 {
				t.Fatalf("read-only cache state caused replacement: %v", plan.RequiresReplace)
			}
			got, err := plan.PlannedState.Unmarshal(typ)
			if err != nil {
				t.Fatal(err)
			}
			var vals map[string]tftypes.Value
			if err := got.As(&vals); err != nil {
				t.Fatal(err)
			}
			if !vals["cache_enabled"].Equal(tc.cache) {
				t.Fatalf("cache state drift in plan: got %v, want %v", vals["cache_enabled"], tc.cache)
			}
		})
	}
}
