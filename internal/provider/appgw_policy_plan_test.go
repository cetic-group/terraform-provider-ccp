package provider

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestAccessPolicyFrameworkPlanningOwnership(t *testing.T) {
	ctx := context.Background()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	for _, name := range []string{"ccp_application_gateway", "ccp_appgw_listener", "ccp_appgw_route"} {
		t.Run(name, func(t *testing.T) {
			typ := sch.ResourceSchemas[name].ValueType().(tftypes.Object)
			obj := func(set map[string]tftypes.Value) tftypes.Value {
				m := map[string]tftypes.Value{}
				for key, at := range typ.AttributeTypes {
					v, ok := set[key]
					if !ok {
						v = tftypes.NewValue(at, nil)
					}
					m[key] = v
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
			run := func(prior, proposed, cfg tftypes.Value) map[string]tftypes.Value {
				res, e := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: name, PriorState: enc(prior), ProposedNewState: enc(proposed), Config: enc(cfg)})
				if e != nil {
					t.Fatal(e)
				}
				for _, d := range res.Diagnostics {
					if d.Severity == tfprotov6.DiagnosticSeverityError {
						t.Fatalf("%s: %s", d.Summary, d.Detail)
					}
				}
				if len(res.RequiresReplace) > 0 {
					t.Fatalf("policy must update in place: %v", res.RequiresReplace)
				}
				raw, e := res.PlannedState.Unmarshal(typ)
				if e != nil {
					t.Fatal(e)
				}
				var values map[string]tftypes.Value
				if e = raw.As(&values); e != nil {
					t.Fatal(e)
				}
				return values
			}
			cfg := map[string]tftypes.Value{}
			switch name {
			case "ccp_application_gateway":
				cfg = map[string]tftypes.Value{"name": str("web"), "region": str("RNN"), "plan": str("appgw-small"), "vpc_id": str("vpc"), "vnet_id": str("vnet")}
			case "ccp_appgw_listener":
				cfg = map[string]tftypes.Value{"appgw_id": str("gw"), "hostname": str("api.example.com")}
			case "ccp_appgw_route":
				cfg = map[string]tftypes.Value{"appgw_id": str("gw"), "listener_id": str("listener"), "target_group_id": str("target")}
			}
			prior := run(tftypes.NewValue(typ, nil), obj(cfg), obj(cfg))
			for key, v := range prior {
				if !v.IsKnown() {
					prior[key] = tftypes.NewValue(v.Type(), nil)
				}
			}
			prior["id"] = str("existing")
			remote, e := policyValue(policyFor("192.0.2.0/24")).ToTerraformValue(ctx)
			if e != nil {
				t.Fatal(e)
			}
			prior["l7_policy"] = remote
			// Omission after import or removal of the attribute relinquishes ownership.
			planned := run(obj(prior), obj(prior), obj(cfg))
			if !planned["l7_policy"].Equal(remote) {
				t.Fatal("omitted policy caused a destructive plan")
			}
			empty, e := policyValue(&client.AppGWL7Policy{}).ToTerraformValue(ctx)
			if e != nil {
				t.Fatal(e)
			}
			proposed := map[string]tftypes.Value{}
			for k, v := range prior {
				proposed[k] = v
			}
			proposed["l7_policy"] = empty
			cfg["l7_policy"] = empty
			planned = run(obj(prior), obj(proposed), obj(cfg))
			if !planned["l7_policy"].Equal(empty) {
				t.Fatal("explicit reset not preserved")
			}
			unresolved := tftypes.NewValue(typ.AttributeTypes["l7_policy"], tftypes.UnknownValue)
			proposed["l7_policy"] = unresolved
			cfg["l7_policy"] = unresolved
			planned = run(obj(prior), obj(proposed), obj(cfg))
			if planned["l7_policy"].IsKnown() {
				t.Fatal("unknown configuration was substituted by an old policy")
			}
		})
	}
}
