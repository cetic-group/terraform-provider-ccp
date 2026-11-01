package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/appgwpolicy"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/resources/appgwlistener"
	"github.com/cetic-group/terraform-provider-ccp/internal/resources/appgwroute"
	"github.com/cetic-group/terraform-provider-ccp/internal/resources/applicationgateway"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests execute real framework CRUD and ownership decisions. The mock
// represents desired state separately from the final applied read, so accepting
// a PATCH cannot accidentally count as convergence.
type policyFixture struct {
	mu          sync.Mutex
	kind        string
	generation  int64
	policy      *client.AppGWL7Policy
	l4          *client.AppGWL4Policy
	clientIP    *client.AppGWClientIPPolicy
	overwriteL4 bool
	caps        bool
	overwrite   bool
	writes      []map[string]any
	methods     []string
}

func policyFor(network string) *client.AppGWL7Policy {
	return &client.AppGWL7Policy{Enabled: true, Policies: []client.AppGWHTTPPolicy{{Key: "private", AllowRules: []client.AppGWSecurityRule{{Key: "office", Match: client.AppGWSecurityMatch{SourceCIDRs: []string{network}}}}}}}
}
func (f *policyFixture) child(kind string) map[string]any {
	m := map[string]any{"id": kind, "appgw_id": "gw", "l7_policy": f.policy, "security_revision": f.generation, "created_at": "2026-10-01T00:00:00Z"}
	if kind == "listener" {
		m["hostname"] = "api.example.com"
		m["acme_challenge"] = "custom"
		m["acme_status"] = "issued"
	} else {
		m["listener_id"] = "listener"
		m["target_group_id"] = "target"
		m["priority"] = 100
		m["path_match_type"] = "prefix"
		m["path_match"] = "/"
		m["waf_preset"] = "off"
	}
	return m
}
func (f *policyFixture) gateway() map[string]any {
	m := map[string]any{"id": "gw", "name": "web", "region": "RNN", "plan": "appgw-small", "vnet_id": "vnet", "status": "active", "replica_count": 1, "vip_address": "10.0.0.10", "force_https": true, "hsts_max_age": 31536000, "tags": []string{}, "created_at": "2026-10-01T00:00:00Z", "l4_policy": map[string]any{}, "client_ip_policy": map[string]any{}, "l7_policy": f.policy, "security_revision": f.generation, "access_policy_generation": f.generation}
	if f.l4 != nil {
		m["l4_policy"] = f.l4
	}
	if f.clientIP != nil {
		m["client_ip_policy"] = f.clientIP
	}
	m["access_policy_status"] = map[string]any{"state": "applied", "generation": f.generation, "replicas": map[string]any{"primary": map[string]any{"state": "applied", "generation": f.generation, "revocation_complete": true}}}
	m["listeners"] = []any{f.child("listener")}
	m["routes"] = []any{f.child("route")}
	return m
}
func (f *policyFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var out any
	if r.URL.Path == "/v1/platform/capabilities" {
		versions := []int{}
		if f.caps {
			versions = []int{1}
		}
		out = map[string]any{"appgw_security_policy_versions": versions, "appgw_security_policies_enabled": f.caps}
	} else if r.Method == http.MethodGet {
		if f.overwrite && len(f.writes) > 0 && r.URL.Path == "/v1/app-gateways/gw" {
			f.policy = &client.AppGWL7Policy{}
			f.generation++
		}
		if f.overwriteL4 && len(f.writes) > 0 && r.URL.Path == "/v1/app-gateways/gw" {
			f.l4 = &client.AppGWL4Policy{}
			f.generation++
		}
		switch r.URL.Path {
		case "/v1/app-gateways/gw":
			out = f.gateway()
		case "/v1/app-gateways/gw/listeners":
			out = []any{f.child("listener")}
		case "/v1/app-gateways/gw/routes":
			out = []any{f.child("route")}
		default:
			t.Errorf("unexpected GET %s", r.URL.Path)
		}
	} else {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		f.writes = append(f.writes, body)
		f.methods = append(f.methods, r.Method+" "+r.URL.Path)
		if p, ok := body["l7_policy"]; ok {
			if r.URL.Path != "/v1/app-gateways" && body["security_revision"] != float64(f.generation) {
				t.Errorf("missing/stale revision: %v", body)
			}
			raw, _ := json.Marshal(p)
			var value client.AppGWL7Policy
			_ = json.Unmarshal(raw, &value)
			f.policy = &value
		}
		if p, ok := body["l4_policy"]; ok {
			raw, _ := json.Marshal(p)
			var policy client.AppGWL4Policy
			_ = json.Unmarshal(raw, &policy)
			f.l4 = &policy
		}
		if p, ok := body["client_ip_policy"]; ok {
			raw, _ := json.Marshal(p)
			var policy client.AppGWClientIPPolicy
			_ = json.Unmarshal(raw, &policy)
			f.clientIP = &policy
		}
		f.generation++
		if f.kind == "gateway" {
			out = f.gateway()
		} else {
			out = f.child(f.kind)
		}
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		t.Errorf("encode: %v", err)
	}
}
func policyResource(kind string) resource.Resource {
	switch kind {
	case "gateway":
		return applicationgateway.New()
	case "listener":
		return appgwlistener.New()
	default:
		return appgwroute.New()
	}
}
func policyHarness(t *testing.T, kind string) (resource.Resource, tfsdk.State, *policyFixture) {
	t.Helper()
	ctx := context.Background()
	f := &policyFixture{kind: kind, generation: 5, policy: policyFor("192.0.2.0/24"), caps: true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(t, w, r) }))
	t.Cleanup(srv.Close)
	r := policyResource(kind)
	var configured resource.ConfigureResponse
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: client.New(srv.URL, "unit", "test")}, &configured)
	noPolicyErrors(t, configured.Diagnostics)
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	st := tfsdk.State{Schema: sch.Schema}
	attrs := map[string]attr.Value{}
	typ := sch.Schema.Type().(types.ObjectType)
	for name, at := range typ.AttrTypes {
		v, err := at.ValueFromTerraform(ctx, tftypes.NewValue(at.TerraformType(ctx), nil))
		if err != nil {
			t.Fatal(err)
		}
		attrs[name] = v
	}
	noPolicyErrors(t, st.Set(ctx, types.ObjectValueMust(typ.AttrTypes, attrs)))
	id := kind
	if kind == "gateway" {
		id = "gw"
	} else {
		noPolicyErrors(t, st.SetAttribute(ctx, path.Root("appgw_id"), "gw"))
	}
	noPolicyErrors(t, st.SetAttribute(ctx, path.Root("id"), id))
	read := resource.ReadResponse{State: st}
	r.Read(ctx, resource.ReadRequest{State: st}, &read)
	noPolicyErrors(t, read.Diagnostics)
	return r, read.State, f
}
func noPolicyErrors(t *testing.T, d diag.Diagnostics) {
	t.Helper()
	if d.HasError() {
		t.Fatalf("diagnostics: %v", d)
	}
}
func policyValue(p *client.AppGWL7Policy) types.Object {
	return appgwpolicy.FromL7(types.ObjectNull(appgwpolicy.L7Type().AttrTypes), p)
}
func updatePolicyResource(t *testing.T, r resource.Resource, st, pl, cfg tfsdk.State) resource.UpdateResponse {
	t.Helper()
	resp := resource.UpdateResponse{State: st}
	r.Update(context.Background(), resource.UpdateRequest{State: st, Plan: tfsdk.Plan{Raw: pl.Raw, Schema: pl.Schema}, Config: tfsdk.Config{Raw: cfg.Raw, Schema: cfg.Schema}}, &resp)
	return resp
}
func TestAccessPolicyResourceLifecycle(t *testing.T) {
	for _, kind := range []string{"gateway", "listener", "route"} {
		t.Run(kind, func(t *testing.T) {
			r, st, f := policyHarness(t, kind)
			ctx := context.Background()
			// Create carries the explicitly configured policy and waits for applied state.
			pl := st
			desired := policyValue(policyFor("198.51.100.0/24"))
			noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l7_policy"), desired))
			cr := resource.CreateResponse{State: tfsdk.State{Schema: st.Schema}}
			r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: pl.Raw, Schema: pl.Schema}, Config: tfsdk.Config{Raw: pl.Raw, Schema: pl.Schema}}, &cr)
			noPolicyErrors(t, cr.Diagnostics)
			if len(f.writes) != 1 || !strings.HasPrefix(f.methods[0], "POST ") {
				t.Fatalf("create writes: %v", f.methods)
			}
			var got types.Object
			noPolicyErrors(t, cr.State.GetAttribute(ctx, path.Root("l7_policy"), &got))
			if !got.Equal(desired) {
				t.Fatal("create lost policy")
			}
			st = cr.State
			// In-place replacement preserves the resource identity and uses a fresh revision.
			pl = st
			desired = policyValue(policyFor("203.0.113.0/24"))
			noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l7_policy"), desired))
			u := updatePolicyResource(t, r, st, pl, pl)
			noPolicyErrors(t, u.Diagnostics)
			st = u.State
			if len(f.writes) != 2 || !strings.HasPrefix(f.methods[1], "PATCH ") {
				t.Fatalf("update replaced resource: %v", f.methods)
			}
			// {} explicitly clears the local policy, and survives a refresh without drift.
			pl = st
			empty := policyValue(&client.AppGWL7Policy{})
			noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l7_policy"), empty))
			u = updatePolicyResource(t, r, st, pl, pl)
			noPolicyErrors(t, u.Diagnostics)
			st = u.State
			if body, ok := f.writes[2]["l7_policy"].(map[string]any); !ok || len(body) != 0 {
				t.Fatalf("reset must send {}, got %v", f.writes[2])
			}
			read := resource.ReadResponse{State: st}
			r.Read(ctx, resource.ReadRequest{State: st}, &read)
			noPolicyErrors(t, read.Diagnostics)
			noPolicyErrors(t, read.State.GetAttribute(ctx, path.Root("l7_policy"), &got))
			if !got.Equal(empty) {
				t.Fatal("empty reset causes refresh drift")
			}
		})
	}
}
func TestAccessPolicyImportOmissionAndUnresolvedValues(t *testing.T) {
	for _, kind := range []string{"gateway", "listener", "route"} {
		t.Run(kind, func(t *testing.T) {
			r, st, f := policyHarness(t, kind)
			ctx := context.Background()
			pl, cfg := st, st
			noPolicyErrors(t, cfg.SetAttribute(ctx, path.Root("l7_policy"), types.ObjectNull(appgwpolicy.L7Type().AttrTypes)))
			switch kind {
			case "gateway":
				noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("name"), "renamed"))
			case "route":
				noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("priority"), 200))
			case "listener":
				noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("tls_certificate_pem"), "CERT"))
				noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("tls_private_key_pem"), "KEY"))
			}
			u := updatePolicyResource(t, r, st, pl, cfg)
			noPolicyErrors(t, u.Diagnostics)
			if len(f.writes) != 1 {
				t.Fatalf("expected ordinary update: %v", f.methods)
			}
			if _, present := f.writes[0]["l7_policy"]; present {
				t.Fatal("omitted imported policy was rewritten")
			}
			// Terraform unknown cannot turn into a destructive reset.
			pl = st
			noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l7_policy"), types.ObjectUnknown(appgwpolicy.L7Type().AttrTypes)))
			u = updatePolicyResource(t, r, st, pl, pl)
			if !u.Diagnostics.HasError() || len(f.writes) != 1 {
				t.Fatalf("unknown policy submitted: %v", u.Diagnostics)
			}
		})
	}
}
func TestAccessPolicyMissingCapabilityAndConcurrentReplacement(t *testing.T) {
	for _, kind := range []string{"gateway", "listener", "route"} {
		t.Run(kind, func(t *testing.T) {
			r, st, f := policyHarness(t, kind)
			ctx := context.Background()
			pl := st
			noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l7_policy"), policyValue(policyFor("198.51.100.0/24"))))
			f.caps = false
			u := updatePolicyResource(t, r, st, pl, pl)
			if !u.Diagnostics.HasError() || len(f.writes) != 0 {
				t.Fatal("missing capability allowed a write")
			}
			f.caps = true
			f.overwrite = true
			u = updatePolicyResource(t, r, st, pl, pl)
			if !u.Diagnostics.HasError() {
				t.Fatal("a later applied generation with overwritten policy counted as success")
			}
			var id types.String
			noPolicyErrors(t, u.State.GetAttribute(ctx, path.Root("id"), &id))
			if id.IsNull() || id.ValueString() == "" {
				t.Fatal("accepted write lost resource from state")
			}
		})
	}
}

func TestAccessPolicyGatewayConfirmsUnchangedOwnedFields(t *testing.T) {
	r, st, f := policyHarness(t, "gateway")
	ctx := context.Background()
	pl := st
	l4 := appgwpolicy.FromL4(types.ObjectNull(appgwpolicy.L4Type().AttrTypes), &client.AppGWL4Policy{Enabled: true, TCP443: &client.AppGWTCPPolicy{AllowCIDRs: []string{"192.0.2.0/24"}}})
	ip := appgwpolicy.FromClientIP(types.ObjectNull(appgwpolicy.ClientIPType().AttrTypes), &client.AppGWClientIPPolicy{Mode: "trusted_xff", TrustedProxyCIDRs: []string{"198.51.100.0/24"}})
	noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l4_policy"), l4))
	noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("client_ip_policy"), ip))
	u := updatePolicyResource(t, r, st, pl, pl)
	noPolicyErrors(t, u.Diagnostics)
	st = u.State
	if _, ok := f.writes[0]["l4_policy"]; !ok {
		t.Fatal("missing L4 write")
	}
	if _, ok := f.writes[0]["client_ip_policy"]; !ok {
		t.Fatal("missing client IP write")
	}
	// Only L7 changes in this plan; an external replacement of managed L4 must
	// still fail confirmation, even if a newer generation is fully applied.
	f.writes = nil
	f.methods = nil
	f.overwriteL4 = true
	pl = st
	noPolicyErrors(t, pl.SetAttribute(ctx, path.Root("l7_policy"), policyValue(policyFor("203.0.113.0/24"))))
	u = updatePolicyResource(t, r, st, pl, pl)
	if !u.Diagnostics.HasError() {
		t.Fatal("unchanged managed L4 was overwritten without an error")
	}
	if _, ok := f.writes[0]["l4_policy"]; ok {
		t.Fatal("unchanged L4 should be confirmed but not rewritten")
	}
}
