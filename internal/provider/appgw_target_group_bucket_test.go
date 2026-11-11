package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Bucket target groups (kind = "bucket"), exercised through the real framework
// protocol: validation, defaults and plan modifiers are the ones users get.

type tgHarness struct {
	t   *testing.T
	srv tfprotov6.ProviderServer
	typ tftypes.Object
}

func newTGHarness(t *testing.T) *tgHarness {
	t.Helper()
	srv, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	sch, err := srv.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return &tgHarness{t: t, srv: srv, typ: sch.ResourceSchemas["ccp_appgw_target_group"].ValueType().(tftypes.Object)}
}

func (h *tgHarness) obj(set map[string]tftypes.Value) tftypes.Value {
	m := map[string]tftypes.Value{}
	for k, at := range h.typ.AttributeTypes {
		if v, ok := set[k]; ok {
			m[k] = v
		} else {
			m[k] = tftypes.NewValue(at, nil)
		}
	}
	return tftypes.NewValue(h.typ, m)
}

func (h *tgHarness) enc(v tftypes.Value) *tfprotov6.DynamicValue {
	d, err := tfprotov6.NewDynamicValue(h.typ, v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func errors(diags []*tfprotov6.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			out = append(out, d.Summary+": "+d.Detail)
		}
	}
	return out
}

func (h *tgHarness) validate(cfg map[string]tftypes.Value) []string {
	r, err := h.srv.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
		TypeName: "ccp_appgw_target_group", Config: h.enc(h.obj(cfg))})
	if err != nil {
		h.t.Fatal(err)
	}
	return errors(r.Diagnostics)
}

func tfs(v string) tftypes.Value          { return tftypes.NewValue(tftypes.String, v) }
func tfb(v bool) tftypes.Value            { return tftypes.NewValue(tftypes.Bool, v) }
func tfn(v int64) tftypes.Value           { return tftypes.NewValue(tftypes.Number, v) }
func tfnull(t tftypes.Type) tftypes.Value { return tftypes.NewValue(t, nil) }

const tgBucketUUID = "11111111-2222-3333-4444-555555555555"

func TestBucketTargetGroupConfigValidation(t *testing.T) {
	h := newTGHarness(t)
	base := func(extra map[string]tftypes.Value) map[string]tftypes.Value {
		c := map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("assets")}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}
	ok := []struct {
		name string
		cfg  map[string]tftypes.Value
	}{
		{"legacy upstream", base(nil)},
		{"explicit upstream", base(map[string]tftypes.Value{"kind": tfs("upstream"), "algorithm": tfs("leastconn")})},
		{"bucket", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID)})},
		{"bucket with folder", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs("site/assets-1.0")})},
		{"unknown kind skips cross checks", base(map[string]tftypes.Value{"kind": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "algorithm": tfs("leastconn")})},
	}
	for _, tc := range ok {
		t.Run("ok/"+tc.name, func(t *testing.T) {
			if e := h.validate(tc.cfg); len(e) != 0 {
				t.Fatalf("unexpected errors: %v", e)
			}
		})
	}
	bad := []struct {
		name string
		cfg  map[string]tftypes.Value
		want string
	}{
		{"unknown kind value", base(map[string]tftypes.Value{"kind": tfs("lambda")}), "kind"},
		{"bucket without bucket_id", base(map[string]tftypes.Value{"kind": tfs("bucket")}), "bucket_id is required"},
		{"upstream with bucket_id", base(map[string]tftypes.Value{"bucket_id": tfs(tgBucketUUID)}), "requires kind"},
		{"upstream with folder", base(map[string]tftypes.Value{"kind": tfs("upstream"), "bucket_prefix": tfs("a")}), "requires kind"},
		{"bucket with algorithm", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "algorithm": tfs("roundrobin")}), "does not apply"},
		{"bucket with sticky", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "sticky_enabled": tfb(false)}), "does not apply"},
		{"bucket with health check", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "hc_interval_sec": tfn(5)}), "does not apply"},
		{"folder with dot-dot", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs("a/../b")}), "segment"},
		{"folder with space", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs("a b")}), "may only contain"},
		{"folder too long", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs(strings.Repeat("a", 513))}), "too long"},
		{"folder not canonical", base(map[string]tftypes.Value{"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs("/site/")}), "canonical"},
	}
	for _, tc := range bad {
		t.Run("bad/"+tc.name, func(t *testing.T) {
			e := h.validate(tc.cfg)
			if len(e) == 0 {
				t.Fatal("config must be rejected at plan time")
			}
			if !strings.Contains(strings.Join(e, "\n"), tc.want) {
				t.Fatalf("errors %v do not mention %q", e, tc.want)
			}
		})
	}
}

func (h *tgHarness) plan(prior tftypes.Value, cfg, proposed map[string]tftypes.Value) (map[string]tftypes.Value, []string) {
	r, err := h.srv.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: "ccp_appgw_target_group", PriorState: h.enc(prior),
		ProposedNewState: h.enc(h.obj(proposed)), Config: h.enc(h.obj(cfg))})
	if err != nil {
		h.t.Fatal(err)
	}
	if e := errors(r.Diagnostics); len(e) != 0 {
		h.t.Fatalf("plan errors: %v", e)
	}
	v, err := r.PlannedState.Unmarshal(h.typ)
	if err != nil {
		h.t.Fatal(err)
	}
	planned := map[string]tftypes.Value{}
	if err = v.As(&planned); err != nil {
		h.t.Fatal(err)
	}
	var replace []string
	for _, p := range r.RequiresReplace {
		replace = append(replace, p.String())
	}
	return planned, replace
}

// existingBucketState is a bucket target group as stored after an apply.
func existingBucketState() map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"id": tfs("tg-1"), "appgw_id": tfs("gw"), "name": tfs("assets"),
		"algorithm": tfs("roundrobin"), "hc_protocol": tfs("http"), "hc_method": tfs("GET"), "hc_path": tfs("/"),
		"hc_expect_status": tfn(200), "hc_interval_sec": tfn(5), "hc_timeout_sec": tfn(3),
		"hc_healthy_threshold": tfn(2), "hc_unhealthy_threshold": tfn(3), "sticky_enabled": tfb(false),
		"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs("site"),
		"origin_status": tfs("ok"),
	}
}

func merge(a, b map[string]tftypes.Value) map[string]tftypes.Value {
	out := map[string]tftypes.Value{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestBucketTargetGroupImmutability(t *testing.T) {
	h := newTGHarness(t)
	state := existingBucketState()
	cfgOf := func(extra map[string]tftypes.Value) map[string]tftypes.Value {
		return merge(map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("assets"),
			"kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID), "bucket_prefix": tfs("site")}, extra)
	}
	plan := func(cfg map[string]tftypes.Value) (map[string]tftypes.Value, []string) {
		return h.plan(h.obj(state), cfg, merge(state, cfg))
	}

	if _, replace := plan(cfgOf(nil)); len(replace) != 0 {
		t.Fatalf("unchanged config must not replace: %v", replace)
	}
	if planned, replace := plan(cfgOf(map[string]tftypes.Value{"bucket_prefix": tfs("site/v2")})); len(replace) != 0 ||
		!planned["bucket_prefix"].Equal(tfs("site/v2")) {
		t.Fatalf("folder must change in place: replace=%v planned=%v", replace, planned["bucket_prefix"])
	}
	if _, replace := plan(cfgOf(map[string]tftypes.Value{"bucket_id": tfs("99999999-2222-3333-4444-555555555555")})); len(replace) != 1 || replace[0] != `AttributeName("bucket_id")` {
		t.Fatalf("bucket_id must force replacement, got %v", replace)
	}
	// kind: bucket -> upstream (bucket_id dropped from the config).
	cfg := map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("assets"), "kind": tfs("upstream")}
	if _, replace := h.plan(h.obj(state), cfg, merge(state, merge(cfg, map[string]tftypes.Value{"bucket_id": tfnull(tftypes.String)}))); !contains(replace, `AttributeName("kind")`) {
		t.Fatalf("kind must force replacement, got %v", replace)
	}
	// Removing `kind` from the configuration of a bucket target is also a change of kind.
	cfg = map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("assets")}
	planned, replace := h.plan(h.obj(state), cfg, merge(state, cfg))
	if !contains(replace, `AttributeName("kind")`) || !planned["kind"].Equal(tfs("upstream")) {
		t.Fatalf("dropping kind must replace and plan upstream: %v %v", replace, planned["kind"])
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// A state written by a provider that predates bucket targets has no kind,
// bucket_id, bucket_prefix or origin fields. Planned as is (-refresh=false), a
// server target group must produce no change at all and no replacement.
func TestLegacyUpstreamStateProducesNoDiff(t *testing.T) {
	h := newTGHarness(t)
	legacy := existingBucketState()
	for _, k := range []string{"kind", "bucket_id", "bucket_prefix", "origin_status", "origin_error"} {
		delete(legacy, k)
	}
	legacyObj := h.obj(legacy)
	cfgs := map[string]map[string]tftypes.Value{
		"kind omitted":  {"appgw_id": tfs("gw"), "name": tfs("assets")},
		"explicit kind": {"appgw_id": tfs("gw"), "name": tfs("assets"), "kind": tfs("upstream")},
	}
	for name, cfg := range cfgs {
		t.Run(name, func(t *testing.T) {
			planned, replace := h.plan(legacyObj, cfg, merge(legacy, cfg))
			if len(replace) != 0 {
				t.Fatalf("legacy state must not be replaced: %v", replace)
			}
			if name == "kind omitted" {
				// Nothing is planned on top of the stored value: the plan equals the state.
				if !tftypes.NewValue(h.typ, planned).Equal(legacyObj) {
					t.Fatalf("plan differs from the legacy state: %v", planned)
				}
			}
		})
	}
	// A legacy state must not stop the user from moving to a bucket target.
	cfg := map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("assets"), "kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID)}
	if _, replace := h.plan(legacyObj, cfg, merge(legacy, cfg)); !contains(replace, `AttributeName("kind")`) {
		t.Fatalf("switching a legacy target group to bucket must replace it: %v", replace)
	}
}

// A brand new bucket target gets its defaults and computed placeholders.
func TestBucketTargetGroupCreatePlan(t *testing.T) {
	h := newTGHarness(t)
	cfg := map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("assets"), "kind": tfs("bucket"), "bucket_id": tfs(tgBucketUUID)}
	planned, replace := h.plan(tftypes.NewValue(h.typ, nil), cfg, cfg)
	if len(replace) != 0 {
		t.Fatalf("creation does not replace: %v", replace)
	}
	if !planned["bucket_prefix"].Equal(tfs("")) {
		t.Fatalf("folder defaults to the whole bucket: %v", planned["bucket_prefix"])
	}
	if planned["origin_status"].IsKnown() {
		t.Fatal("origin_status is known only after apply")
	}
	up := map[string]tftypes.Value{"appgw_id": tfs("gw"), "name": tfs("pool")}
	planned, _ = h.plan(tftypes.NewValue(h.typ, nil), up, up)
	if !planned["kind"].Equal(tfs("upstream")) {
		t.Fatalf("kind defaults to upstream: %v", planned["kind"])
	}
}
