package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// ACME validation changes still require replacement until the staged API is
// enabled and the provider has a tested in-place lifecycle. The API endpoint
// alone is not sufficient to remove these plan modifiers safely.
func TestAppGWListenerACMEChangesStillRequireReplacement(t *testing.T) {
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
	creds := func(token string) tftypes.Value {
		return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String},
			map[string]tftypes.Value{"api_token": str(token)})
	}
	base := map[string]tftypes.Value{
		"appgw_id": str("gw-1"), "hostname": str("api.example.com"),
		"acme_challenge": str("dns01"), "acme_dns_provider": str("cloudflare"),
		"acme_dns_credentials": creds("original"),
	}
	created, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName: "ccp_appgw_listener", PriorState: enc(tftypes.NewValue(typ, nil)),
		ProposedNewState: enc(obj(base)), Config: enc(obj(base)),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoACMEPlanErrors(t, created)
	planned, err := created.PlannedState.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := tftypes.Transform(planned, func(p *tftypes.AttributePath, v tftypes.Value) (tftypes.Value, error) {
		if len(p.Steps()) == 1 && p.Steps()[0] == tftypes.AttributeName("id") {
			return str("listener-1"), nil
		}
		if !v.IsKnown() {
			return tftypes.NewValue(v.Type(), nil), nil
		}
		return v, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var priorValues map[string]tftypes.Value
	if err := prior.As(&priorValues); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		changed     map[string]tftypes.Value
		mustReplace string
	}{
		{"method", map[string]tftypes.Value{
			"acme_challenge":       str("http01"),
			"acme_dns_provider":    tftypes.NewValue(tftypes.String, nil),
			"acme_dns_credentials": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, nil),
		}, "acme_challenge"},
		{"provider", map[string]tftypes.Value{"acme_dns_provider": str("ovh")}, "acme_dns_provider"},
		{"credentials", map[string]tftypes.Value{"acme_dns_credentials": creds("replacement")}, "acme_dns_credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]tftypes.Value{}
			for k, v := range base {
				cfg[k] = v
			}
			proposed := map[string]tftypes.Value{}
			for k, v := range priorValues {
				proposed[k] = v
			}
			for k, v := range tc.changed {
				cfg[k], proposed[k] = v, v
			}
			result, err := srv.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName: "ccp_appgw_listener", PriorState: enc(prior),
				ProposedNewState: enc(obj(proposed)), Config: enc(obj(cfg)),
			})
			if err != nil {
				t.Fatal(err)
			}
			assertNoACMEPlanErrors(t, result)
			wanted := tftypes.NewAttributePath().WithAttributeName(tc.mustReplace)
			for _, path := range result.RequiresReplace {
				if path.Equal(wanted) {
					return
				}
			}
			t.Fatalf("%s must require replacement, got %v", tc.mustReplace, result.RequiresReplace)
		})
	}
}

func assertNoACMEPlanErrors(t *testing.T, plan *tfprotov6.PlanResourceChangeResponse) {
	t.Helper()
	for _, d := range plan.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan: %s: %s", d.Summary, d.Detail)
		}
	}
}
