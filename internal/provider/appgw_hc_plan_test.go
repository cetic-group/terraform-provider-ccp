package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestAppGWTargetGroupHealthCheckPlan(t *testing.T) {
	ctx := context.Background()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	typ := sch.ResourceSchemas["ccp_appgw_target_group"].ValueType().(tftypes.Object)
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
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	bl := func(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }
	base := map[string]tftypes.Value{"appgw_id": str("gw-1"), "name": str("pool")}
	create, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: "ccp_appgw_target_group", PriorState: enc(tftypes.NewValue(typ, nil)),
		ProposedNewState: enc(obj(base)), Config: enc(obj(base)),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoPlanErrors(t, create)
	planned, err := create.PlannedState.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]tftypes.Value
	if err := planned.As(&values); err != nil {
		t.Fatal(err)
	}
	if !values["hc_enabled"].Equal(bl(true)) {
		t.Fatalf("legacy config must plan hc_enabled=true, got %v", values["hc_enabled"])
	}
	values["id"] = str("tg-1")
	prior := obj(values)

	configured := map[string]tftypes.Value{"appgw_id": str("gw-1"), "name": str("pool"), "hc_enabled": bl(false)}
	proposed := map[string]tftypes.Value{}
	for k, v := range values {
		proposed[k] = v
	}
	proposed["hc_enabled"] = bl(false)
	change, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: "ccp_appgw_target_group", PriorState: enc(prior),
		ProposedNewState: enc(obj(proposed)), Config: enc(obj(configured)),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoPlanErrors(t, change)
	if len(change.RequiresReplace) != 0 {
		t.Fatalf("disabling hc_enabled must update in place, got replacement: %v", change.RequiresReplace)
	}
	changed, err := change.PlannedState.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	var changedValues map[string]tftypes.Value
	if err := changed.As(&changedValues); err != nil {
		t.Fatal(err)
	}
	if !changedValues["hc_enabled"].Equal(bl(false)) {
		t.Fatalf("explicit hc_enabled=false was not planned: %v", changedValues["hc_enabled"])
	}
}

func assertNoPlanErrors(t *testing.T, plan *tfprotov6.PlanResourceChangeResponse) {
	t.Helper()
	for _, d := range plan.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan error: %s: %s", d.Summary, d.Detail)
		}
	}
}
