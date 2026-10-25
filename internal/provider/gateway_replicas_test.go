package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Use the framework protocol, including defaults and plan modifiers, rather
// than asserting the presence of RequiresReplace in a schema declaration.
func TestGatewayReplicaPlanning(t *testing.T) {
	ctx := context.Background()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range []string{"ccp_application_gateway", "ccp_load_balancer"} {
		t.Run(rt, func(t *testing.T) {
			typ := schema.ResourceSchemas[rt].ValueType().(tftypes.Object)
			str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
			num := func(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
			obj := func(set map[string]tftypes.Value) tftypes.Value {
				m := map[string]tftypes.Value{}
				for key, at := range typ.AttributeTypes {
					if v, ok := set[key]; ok {
						m[key] = v
					} else {
						m[key] = tftypes.NewValue(at, nil)
					}
				}
				return tftypes.NewValue(typ, m)
			}
			enc := func(v tftypes.Value) *tfprotov6.DynamicValue {
				d, e := tfprotov6.NewDynamicValue(typ, v)
				if e != nil {
					t.Fatal(e)
				}
				return &d
			}
			check := func(r *tfprotov6.PlanResourceChangeResponse) map[string]tftypes.Value {
				for _, d := range r.Diagnostics {
					if d.Severity == tfprotov6.DiagnosticSeverityError {
						t.Fatalf("%s: %s", d.Summary, d.Detail)
					}
				}
				v, e := r.PlannedState.Unmarshal(typ)
				if e != nil {
					t.Fatal(e)
				}
				m := map[string]tftypes.Value{}
				if e = v.As(&m); e != nil {
					t.Fatal(e)
				}
				return m
			}
			cfg := map[string]tftypes.Value{"name": str("web"), "region": str("RNN"), "vnet_id": str("vnet"), "plan": str("small")}
			if rt == "ccp_application_gateway" {
				cfg["vpc_id"] = str("vpc")
			}
			create, e := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: rt,
				PriorState: enc(tftypes.NewValue(typ, nil)), ProposedNewState: enc(obj(cfg)), Config: enc(obj(cfg))})
			if e != nil {
				t.Fatal(e)
			}
			prior := check(create)
			if !prior["replica_count"].Equal(num(2)) {
				t.Fatalf("default must preserve HA: %v", prior["replica_count"])
			}
			for k, v := range prior {
				if !v.IsKnown() {
					prior[k] = tftypes.NewValue(v.Type(), nil)
				}
			}
			prior["id"] = str("existing")
			for _, count := range []int64{2, 1} {
				cfg["replica_count"] = num(count)
				proposed := map[string]tftypes.Value{}
				for k, v := range prior {
					proposed[k] = v
				}
				proposed["replica_count"] = num(count)
				result, e := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: rt,
					PriorState: enc(obj(prior)), ProposedNewState: enc(obj(proposed)), Config: enc(obj(cfg))})
				if e != nil {
					t.Fatal(e)
				}
				check(result)
				if count == 2 && len(result.RequiresReplace) != 0 {
					t.Fatalf("unchanged HA replaced: %v", result.RequiresReplace)
				}
				if count == 1 && len(result.RequiresReplace) == 0 {
					t.Fatal("single mode must replace existing HA resource")
				}
			}
			for _, count := range []int64{0, 3} {
				cfg["replica_count"] = num(count)
				result, e := srv.ValidateResourceConfig(ctx, &tfprotov6.ValidateResourceConfigRequest{TypeName: rt, Config: enc(obj(cfg))})
				if e != nil {
					t.Fatal(e)
				}
				failed := false
				for _, d := range result.Diagnostics {
					if d.Severity == tfprotov6.DiagnosticSeverityError {
						failed = true
					}
				}
				if !failed {
					t.Fatalf("replica_count=%d must be rejected", count)
				}
			}
		})
	}
}
