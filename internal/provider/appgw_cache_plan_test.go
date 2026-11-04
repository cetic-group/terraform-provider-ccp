package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Exercise actual framework planning with a state written before cache sizes
// became computed-only. No state upgrader, replacement or pinned old size is needed.
func TestAppGWCachePlanMigration(t *testing.T) {
	ctx := context.Background()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	rt := "ccp_application_gateway"
	typ := sch.ResourceSchemas[rt].ValueType().(tftypes.Object)
	for _, a := range sch.ResourceSchemas[rt].Block.Attributes {
		if a.Name == "cache_size_mb" || a.Name == "cache_max_object_kb" {
			if !a.Computed || a.Optional || a.Required {
				t.Fatalf("size must be computed-only: %+v", a)
			}
		}
	}
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	num := func(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
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
	enc := func(v tftypes.Value) *tfprotov6.DynamicValue {
		d, err := tfprotov6.NewDynamicValue(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	check := func(r *tfprotov6.PlanResourceChangeResponse) map[string]tftypes.Value {
		for _, d := range r.Diagnostics {
			if d.Severity == tfprotov6.DiagnosticSeverityError {
				t.Fatalf("%s: %s", d.Summary, d.Detail)
			}
		}
		if len(r.RequiresReplace) != 0 {
			t.Fatalf("cache migration must not replace gateway: %v", r.RequiresReplace)
		}
		v, err := r.PlannedState.Unmarshal(typ)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]tftypes.Value
		if err = v.As(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	cfg := map[string]tftypes.Value{"name": str("web"), "region": str("RNN"), "vpc_id": str("vpc"), "vnet_id": str("vnet"), "plan": str("appgw-medium")}
	cr, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: rt,
		PriorState: enc(tftypes.NewValue(typ, nil)), ProposedNewState: enc(obj(cfg)), Config: enc(obj(cfg))})
	if err != nil {
		t.Fatal(err)
	}
	prior := check(cr)
	for k, v := range prior {
		if !v.IsKnown() {
			prior[k] = tftypes.NewValue(v.Type(), nil)
		}
	}
	prior["id"] = str("existing")
	prior["cache_size_mb"] = num(256)
	prior["cache_max_object_kb"] = num(512)
	prior["cache_effective_size_mb"] = num(256)
	prior["cache_size_limit_mb"] = num(256)
	for _, planName := range []string{"appgw-medium", "appgw-small", "appgw-custom"} {
		t.Run(planName, func(t *testing.T) {
			cfg["plan"] = str(planName)
			proposed := map[string]tftypes.Value{}
			for k, v := range prior {
				proposed[k] = v
			}
			proposed["plan"] = str(planName)
			result, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: rt,
				PriorState: enc(obj(prior)), ProposedNewState: enc(obj(proposed)), Config: enc(obj(cfg))})
			if err != nil {
				t.Fatal(err)
			}
			planned := check(result)
			for _, key := range []string{"cache_size_mb", "cache_max_object_kb", "cache_effective_size_mb", "cache_size_limit_mb"} {
				if planName == "appgw-medium" {
					if !planned[key].Equal(prior[key]) {
						t.Fatalf("unchanged config caused drift for %s: %v", key, planned[key])
					}
				} else if planned[key].IsKnown() {
					t.Fatalf("changed plan pinned stale %s: %v", key, planned[key])
				}
			}
		})
	}
}
