package appgwlistener

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strp(s string) *string { return &s }

func TestApplyToModel_NoACME(t *testing.T) {
	l := &client.AppGWListener{
		ID:         "l1",
		AppGWID:    "gw1",
		Hostname:   "api.example.com",
		AcmeStatus: "pending",
		CreatedAt:  "2026-06-01T10:00:00Z",
	}
	m := &listenerResourceModel{
		// credentials never set by user
		AcmeDNSCredentials: types.MapNull(types.StringType),
	}
	applyToModel(l, m)

	if m.ID.ValueString() != "l1" || m.AppGWID.ValueString() != "gw1" {
		t.Fatalf("id/appgw mismatch: %+v", m)
	}
	if m.Hostname.ValueString() != "api.example.com" {
		t.Errorf("hostname: %q", m.Hostname.ValueString())
	}
	if m.AcmeStatus.ValueString() != "pending" {
		t.Errorf("acme_status: %q", m.AcmeStatus.ValueString())
	}
	// All optional ACME mirror fields should be Null when absent.
	for name, v := range map[string]types.String{
		"acme_challenge":       m.AcmeChallenge,
		"acme_dns_provider":    m.AcmeDNSProvider,
		"acme_last_renewal_at": m.AcmeLastRenewalAt,
		"acme_issued_at":       m.AcmeIssuedAt,
		"acme_renew_after":     m.AcmeRenewAfter,
		"acme_last_error":      m.AcmeLastError,
		"cert_path":            m.CertPath,
	} {
		if !v.IsNull() {
			t.Errorf("%s should be null, got %q", name, v.ValueString())
		}
	}
	if !m.AcmeDNSCredentials.IsNull() {
		t.Errorf("credentials should remain null")
	}
}

func TestApplyToModel_WithACME(t *testing.T) {
	l := &client.AppGWListener{
		ID:                "l2",
		AppGWID:           "gw1",
		Hostname:          "api.example.com",
		AcmeStatus:        "issued",
		AcmeChallenge:     strp("dns01"),
		AcmeDNSProvider:   strp("cloudflare"),
		AcmeLastRenewalAt: strp("2026-06-01T11:00:00Z"),
		AcmeIssuedAt:      strp("2026-06-01T11:00:00Z"),
		AcmeRenewAfter:    strp("2026-08-01T11:00:00Z"),
		CertPath:          strp("/etc/certs/l2"),
		CreatedAt:         "2026-06-01T10:00:00Z",
	}
	m := &listenerResourceModel{}
	applyToModel(l, m)

	if m.AcmeChallenge.ValueString() != "dns01" {
		t.Errorf("acme_challenge: %q", m.AcmeChallenge.ValueString())
	}
	if m.AcmeDNSProvider.ValueString() != "cloudflare" {
		t.Errorf("acme_dns_provider: %q", m.AcmeDNSProvider.ValueString())
	}
	if m.AcmeIssuedAt.ValueString() != "2026-06-01T11:00:00Z" {
		t.Errorf("acme_issued_at: %q", m.AcmeIssuedAt.ValueString())
	}
	if m.AcmeRenewAfter.ValueString() != "2026-08-01T11:00:00Z" {
		t.Errorf("acme_renew_after: %q", m.AcmeRenewAfter.ValueString())
	}
	if m.CertPath.ValueString() != "/etc/certs/l2" {
		t.Errorf("cert_path: %q", m.CertPath.ValueString())
	}
}

func TestApplyToModel_CredentialsCarriedOver(t *testing.T) {
	// The API never returns credentials. The model already holds them (from
	// plan in Create, or prior state in Read) and applyToModel must not clobber.
	creds, d := types.MapValueFrom(context.Background(), types.StringType, map[string]string{"api_token": "secret"})
	if d.HasError() {
		t.Fatalf("build map: %v", d)
	}
	m := &listenerResourceModel{AcmeDNSCredentials: creds}

	l := &client.AppGWListener{ID: "l3", AppGWID: "gw1", Hostname: "x.example.com", AcmeStatus: "issued", CreatedAt: "t"}
	applyToModel(l, m)

	if m.AcmeDNSCredentials.IsNull() || m.AcmeDNSCredentials.IsUnknown() {
		t.Fatalf("credentials should be carried over, got null/unknown")
	}
	got := map[string]string{}
	m.AcmeDNSCredentials.ElementsAs(context.Background(), &got, false)
	if got["api_token"] != "secret" {
		t.Errorf("credentials not preserved: %+v", got)
	}
}

func TestApplyToModel_UnknownCredentialsNormalizedToNull(t *testing.T) {
	m := &listenerResourceModel{AcmeDNSCredentials: types.MapUnknown(types.StringType)}
	l := &client.AppGWListener{ID: "l4", AppGWID: "gw1", Hostname: "x", AcmeStatus: "pending", CreatedAt: "t"}
	applyToModel(l, m)
	if !m.AcmeDNSCredentials.IsNull() {
		t.Errorf("unknown credentials should be normalized to null")
	}
}

func TestValidateDNS01(t *testing.T) {
	creds, _ := types.MapValueFrom(context.Background(), types.StringType, map[string]string{"api_token": "t"})

	cases := []struct {
		name      string
		model     listenerResourceModel
		wantError bool
	}{
		{
			name:      "challenge null — no validation",
			model:     listenerResourceModel{AcmeChallenge: types.StringNull()},
			wantError: false,
		},
		{
			name:      "challenge unknown — early return",
			model:     listenerResourceModel{AcmeChallenge: types.StringUnknown()},
			wantError: false,
		},
		{
			name: "http01 — provider/credentials not required",
			model: listenerResourceModel{
				AcmeChallenge:      types.StringValue("http01"),
				AcmeDNSProvider:    types.StringNull(),
				AcmeDNSCredentials: types.MapNull(types.StringType),
			},
			wantError: false,
		},
		{
			name: "dns01 missing both",
			model: listenerResourceModel{
				AcmeChallenge:      types.StringValue("dns01"),
				AcmeDNSProvider:    types.StringNull(),
				AcmeDNSCredentials: types.MapNull(types.StringType),
			},
			wantError: true,
		},
		{
			name: "dns01 missing credentials",
			model: listenerResourceModel{
				AcmeChallenge:      types.StringValue("dns01"),
				AcmeDNSProvider:    types.StringValue("cloudflare"),
				AcmeDNSCredentials: types.MapNull(types.StringType),
			},
			wantError: true,
		},
		{
			name: "dns01 empty credentials map",
			model: listenerResourceModel{
				AcmeChallenge:      types.StringValue("dns01"),
				AcmeDNSProvider:    types.StringValue("cloudflare"),
				AcmeDNSCredentials: types.MapValueMust(types.StringType, map[string]attr.Value{}),
			},
			wantError: true,
		},
		{
			name: "dns01 valid",
			model: listenerResourceModel{
				AcmeChallenge:      types.StringValue("dns01"),
				AcmeDNSProvider:    types.StringValue("cloudflare"),
				AcmeDNSCredentials: creds,
			},
			wantError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateDNS01(tc.model)
			if diags.HasError() != tc.wantError {
				t.Errorf("wantError=%v, got diags=%v", tc.wantError, diags)
			}
		})
	}
}

func TestSplitID(t *testing.T) {
	parts := splitID("gw-uuid/listener-uuid")
	if len(parts) != 2 || parts[0] != "gw-uuid" || parts[1] != "listener-uuid" {
		t.Errorf("splitID: %+v", parts)
	}
}
