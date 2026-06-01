// Unit tests for ccp_load_balancer — pure helpers that map the API onto the
// Terraform model, match backends during reconcile, and decide when an
// immutable listener change forces replacement. No framework mocking.
package loadbalancer

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strptr(s string) *string { return &s }

//  1. stateFromAPI maps a listener with ACME fields correctly + carries over
//     the write-only acme_dns_credentials from the prior model.
func TestStateFromAPI_MapsAcmeListener(t *testing.T) {
	ctx := context.Background()

	prior := []lbListenerModel{
		{
			Protocol:   types.StringValue("https"),
			ListenPort: types.Int64Value(443),
			AcmeDNSCredentials: func() types.Map {
				m, _ := types.MapValueFrom(ctx, types.StringType, map[string]string{"api_token": "secret"})
				return m
			}(),
		},
	}

	lb := &client.LoadBalancer{
		ID:     "lb-1",
		Name:   "web",
		Region: "RNN",
		Plan:   "small",
		VnetID: "vnet-1",
		Status: "active",
		Tags:   []string{},
		Listeners: []client.LBListener{
			{
				ID:                 "lst-1",
				Protocol:           "https",
				ListenPort:         443,
				Algorithm:          "roundrobin",
				HealthCheckEnabled: true,
				HealthCheckPath:    strptr("/healthz"),
				Domain:             strptr("example.com"),
				AcmeChallenge:      strptr("dns01"),
				AcmeStatus:         strptr("error"),
				AcmeLastError:      strptr("DNS challenge failed: timeout"),
				AcmeDNSProvider:    strptr("cloudflare"),
				Backends: []client.LBBackend{
					{ID: "be-1", ContainerID: strptr("ct-1"), Port: 8080, Weight: 5},
					{ID: "be-2", VMID: strptr("vm-9"), Port: 8080, Weight: 1},
				},
			},
		},
	}

	m, diags := stateFromAPI(ctx, lb, prior)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if len(m.Listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(m.Listeners))
	}
	l := m.Listeners[0]
	if l.Protocol.ValueString() != "https" || l.ListenPort.ValueInt64() != 443 {
		t.Errorf("protocol/port mismatch: %s:%d", l.Protocol.ValueString(), l.ListenPort.ValueInt64())
	}
	if l.Algorithm.ValueString() != "roundrobin" {
		t.Errorf("algorithm mismatch: %q", l.Algorithm.ValueString())
	}
	if !l.HealthCheckEnabled.ValueBool() {
		t.Errorf("expected health_check_enabled=true")
	}
	if l.HealthCheckPath.ValueString() != "/healthz" {
		t.Errorf("health_check_path mismatch: %q", l.HealthCheckPath.ValueString())
	}
	if l.Domain.ValueString() != "example.com" {
		t.Errorf("domain mismatch: %q", l.Domain.ValueString())
	}
	if l.AcmeChallenge.ValueString() != "dns01" {
		t.Errorf("acme_challenge mismatch: %q", l.AcmeChallenge.ValueString())
	}
	if l.AcmeStatus.ValueString() != "error" {
		t.Errorf("acme_status mismatch: %q", l.AcmeStatus.ValueString())
	}
	if l.AcmeLastError.ValueString() != "DNS challenge failed: timeout" {
		t.Errorf("acme_last_error mismatch: %q", l.AcmeLastError.ValueString())
	}
	if l.AcmeDNSProvider.ValueString() != "cloudflare" {
		t.Errorf("acme_dns_provider mismatch: %q", l.AcmeDNSProvider.ValueString())
	}
	// Write-only credentials must be carried over from the prior model — the
	// API never returns them.
	if l.AcmeDNSCredentials.IsNull() {
		t.Errorf("expected acme_dns_credentials carried over from prior, got null")
	}
	var got map[string]string
	l.AcmeDNSCredentials.ElementsAs(ctx, &got, false)
	if got["api_token"] != "secret" {
		t.Errorf("expected carried-over credential api_token=secret, got %v", got)
	}
	// Backends mapped, container vs vm exclusivity.
	if len(l.Backends) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(l.Backends))
	}
	if l.Backends[0].ContainerID.ValueString() != "ct-1" || !l.Backends[0].VMID.IsNull() {
		t.Errorf("backend 0 should be container ct-1 with null vm, got ct=%q vm=%v",
			l.Backends[0].ContainerID.ValueString(), l.Backends[0].VMID)
	}
	if l.Backends[1].VMID.ValueString() != "vm-9" || !l.Backends[1].ContainerID.IsNull() {
		t.Errorf("backend 1 should be vm vm-9 with null container, got vm=%q ct=%v",
			l.Backends[1].VMID.ValueString(), l.Backends[1].ContainerID)
	}
}

func TestStateFromAPI_NilPriorCredentialsNull(t *testing.T) {
	ctx := context.Background()
	lb := &client.LoadBalancer{
		ID: "lb-1", Name: "x", Region: "RNN", Plan: "small", VnetID: "v", Status: "active", Tags: []string{},
		Listeners: []client.LBListener{
			{ID: "lst", Protocol: "https", ListenPort: 443, Algorithm: "roundrobin", HealthCheckEnabled: true},
		},
	}
	m, _ := stateFromAPI(ctx, lb, nil)
	if !m.Listeners[0].AcmeDNSCredentials.IsNull() {
		t.Errorf("expected null credentials when no prior, got %v", m.Listeners[0].AcmeDNSCredentials)
	}
	// acme_last_error nil on the API → null in state.
	if !m.Listeners[0].AcmeLastError.IsNull() {
		t.Errorf("expected null acme_last_error when API returns nil, got %v", m.Listeners[0].AcmeLastError)
	}
}

// 2. backendKey distinguishes container vs vm and handles port collisions.
func TestBackendKey(t *testing.T) {
	ct := lbBackendModel{ContainerID: types.StringValue("abc"), VMID: types.StringNull(), Port: types.Int64Value(80)}
	vm := lbBackendModel{ContainerID: types.StringNull(), VMID: types.StringValue("abc"), Port: types.Int64Value(80)}
	// Same id string, same port, but container vs vm must NOT collide.
	if backendKey(ct) == backendKey(vm) {
		t.Errorf("container and vm with same id+port must not collide: %q == %q", backendKey(ct), backendKey(vm))
	}
	// Same target, different port → different key.
	ct2 := lbBackendModel{ContainerID: types.StringValue("abc"), VMID: types.StringNull(), Port: types.Int64Value(81)}
	if backendKey(ct) == backendKey(ct2) {
		t.Errorf("different ports must yield different keys")
	}
	// Identical container backends → identical key.
	ctDup := lbBackendModel{ContainerID: types.StringValue("abc"), VMID: types.StringNull(), Port: types.Int64Value(80)}
	if backendKey(ct) != backendKey(ctDup) {
		t.Errorf("identical backends must yield identical keys")
	}
}

//  3. listenersRequireReplace returns true on any immutable listener change,
//     false when only backends / weight / computed fields differ.
func baseListener() lbListenerModel {
	return lbListenerModel{
		ID:                 types.StringValue("lst-1"),
		Protocol:           types.StringValue("https"),
		ListenPort:         types.Int64Value(443),
		Algorithm:          types.StringValue("roundrobin"),
		HealthCheckEnabled: types.BoolValue(true),
		HealthCheckPath:    types.StringValue("/healthz"),
		Domain:             types.StringValue("example.com"),
		AcmeChallenge:      types.StringValue("http01"),
		AcmeDNSProvider:    types.StringNull(),
		AcmeStatus:         types.StringValue("issued"),
		AcmeLastError:      types.StringNull(),
		AcmeDNSCredentials: types.MapNull(types.StringType),
		Backends: []lbBackendModel{
			{ID: types.StringValue("be-1"), ContainerID: types.StringValue("ct-1"), VMID: types.StringNull(), Port: types.Int64Value(8080), Weight: types.Int64Value(1)},
		},
	}
}

func TestListenersRequireReplace(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(l *lbListenerModel)
		want   bool
	}{
		{"no change", func(l *lbListenerModel) {}, false},
		{"only weight changed", func(l *lbListenerModel) { l.Backends[0].Weight = types.Int64Value(5) }, false},
		{"backend added", func(l *lbListenerModel) {
			l.Backends = append(l.Backends, lbBackendModel{ContainerID: types.StringValue("ct-2"), VMID: types.StringNull(), Port: types.Int64Value(8081), Weight: types.Int64Value(1)})
		}, false},
		{"backend removed", func(l *lbListenerModel) { l.Backends = nil }, false},
		{"computed acme_status changed", func(l *lbListenerModel) { l.AcmeStatus = types.StringValue("renewing") }, false},
		{"computed acme_last_error changed", func(l *lbListenerModel) { l.AcmeLastError = types.StringValue("boom") }, false},
		{"protocol changed", func(l *lbListenerModel) { l.Protocol = types.StringValue("http") }, true},
		{"listen_port changed", func(l *lbListenerModel) { l.ListenPort = types.Int64Value(8443) }, true},
		{"algorithm changed", func(l *lbListenerModel) { l.Algorithm = types.StringValue("leastconn") }, true},
		{"health_check_enabled changed", func(l *lbListenerModel) { l.HealthCheckEnabled = types.BoolValue(false) }, true},
		{"health_check_path changed", func(l *lbListenerModel) { l.HealthCheckPath = types.StringValue("/ping") }, true},
		{"domain changed", func(l *lbListenerModel) { l.Domain = types.StringValue("other.com") }, true},
		{"acme_challenge changed", func(l *lbListenerModel) { l.AcmeChallenge = types.StringValue("dns01") }, true},
		{"acme_dns_provider changed", func(l *lbListenerModel) { l.AcmeDNSProvider = types.StringValue("route53") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := []lbListenerModel{baseListener()}
			plan := []lbListenerModel{baseListener()}
			tt.mutate(&plan[0])
			if got := listenersRequireReplace(plan, state); got != tt.want {
				t.Errorf("listenersRequireReplace = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListenersRequireReplace_CountAndKeyChange(t *testing.T) {
	state := []lbListenerModel{baseListener()}

	// Different count → replace.
	if !listenersRequireReplace([]lbListenerModel{baseListener(), baseListener()}, state) {
		t.Errorf("expected replace when listener count differs")
	}
	// Same count but a listener with a brand-new (protocol, port) key replaces
	// one with the old key → replace (key not found in state).
	newKey := baseListener()
	newKey.ListenPort = types.Int64Value(8443)
	if !listenersRequireReplace([]lbListenerModel{newKey}, state) {
		t.Errorf("expected replace when a listener key is not present in state")
	}
}
