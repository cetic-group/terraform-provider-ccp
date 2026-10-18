package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestPlanVIPAddress drives PlanResourceChange through the real provider
// server: the plan modifiers of vip_address decide whether an in-place edit
// replaces the gateway / load balancer — a schema read cannot show that.
func TestPlanVIPAddress(t *testing.T) {
	ctx := context.Background()
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}

	for _, rt := range []string{"ccp_application_gateway", "ccp_load_balancer"} {
		for _, priorVIP := range []*string{ptr("10.0.1.5"), nil} {
			typ := sch.ResourceSchemas[rt].ValueType().(tftypes.Object)
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
			base := map[string]tftypes.Value{"region": str("RNN"), "vnet_id": str("vnet-1"), "name": str("a")}
			if rt == "ccp_application_gateway" {
				base["vpc_id"] = str("vpc-1")
			}

			// Prior state = the create plan with unknowns resolved, as after apply.
			cr, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: rt,
				PriorState: enc(tftypes.NewValue(typ, nil)), ProposedNewState: enc(obj(base)), Config: enc(obj(base))})
			if err != nil {
				t.Fatal(err)
			}
			planned, _ := cr.PlannedState.Unmarshal(typ)
			prior, _ := tftypes.Transform(planned, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
				if len(p.Steps()) == 1 {
					switch string(p.Steps()[0].(tftypes.AttributeName)) {
					case "id":
						return str("x"), nil
					case "vip_address":
						if priorVIP == nil {
							return tftypes.NewValue(tftypes.String, nil), nil
						}
						return str(*priorVIP), nil
					}
				}
				if !v.IsKnown() {
					return tftypes.NewValue(v.Type(), nil), nil
				}
				return v, nil
			})
			var pm map[string]tftypes.Value
			if err := prior.As(&pm); err != nil {
				t.Fatal(err)
			}

			cases := []struct {
				label   string
				over    map[string]tftypes.Value
				replace bool
			}{
				{"no change, vip omitted", nil, false},
				{"rename, vip omitted", map[string]tftypes.Value{"name": str("b")}, false},
				{"vip differs", map[string]tftypes.Value{"vip_address": str("10.0.1.6")}, true},
			}
			if priorVIP != nil {
				cases = append(cases, struct {
					label   string
					over    map[string]tftypes.Value
					replace bool
				}{"rename, vip = state", map[string]tftypes.Value{"name": str("b"), "vip_address": str(*priorVIP)}, false})
			}
			for _, c := range cases {
				cfg := map[string]tftypes.Value{}
				for k, v := range base {
					cfg[k] = v
				}
				prop := map[string]tftypes.Value{}
				for k, v := range pm {
					prop[k] = v
				}
				for k, v := range c.over {
					cfg[k] = v
					prop[k] = v
				}
				r, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: rt,
					PriorState: enc(prior), ProposedNewState: enc(tftypes.NewValue(typ, prop)), Config: enc(obj(cfg))})
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range r.Diagnostics {
					if d.Severity == tfprotov6.DiagnosticSeverityError {
						t.Fatalf("%s %s: %s %s", rt, c.label, d.Summary, d.Detail)
					}
				}
				replaced := false
				for _, p := range r.RequiresReplace {
					if p.Equal(tftypes.NewAttributePath().WithAttributeName("vip_address")) {
						replaced = true
					}
				}
				if replaced != c.replace {
					t.Errorf("%s, prior vip %v, %s: replace=%v, want %v", rt, priorVIP != nil, c.label, replaced, c.replace)
				}
			}
		}
	}
}

func ptr(s string) *string { return &s }
