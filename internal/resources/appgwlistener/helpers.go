package appgwlistener

import (
	"regexp"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// lowercaseRe rejects any uppercase ASCII letter. The backend lowercases the
// hostname server-side, so accepting mixed case would produce an inconsistent
// result after apply.
var lowercaseRe = regexp.MustCompile(`^[^A-Z]*$`)

// strVal returns the string value of a known, non-null, non-empty attribute.
func strVal(s types.String) (string, bool) {
	if s.IsNull() || s.IsUnknown() {
		return "", false
	}
	v := s.ValueString()
	if v == "" {
		return "", false
	}
	return v, true
}

func optStr(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// applyToModel maps an API listener onto the Terraform model. The write-only
// acme_dns_provider/credentials inputs are NOT returned in clear by the API:
// acme_dns_provider is mirrored back (so we map it), but acme_dns_credentials
// is preserved from the prior model value (already in m) and never touched.
func applyToModel(l *client.AppGWListener, m *listenerResourceModel) {
	m.ID = types.StringValue(l.ID)
	m.AppGWID = types.StringValue(l.AppGWID)
	m.Hostname = types.StringValue(l.Hostname)
	m.AcmeStatus = types.StringValue(l.AcmeStatus)
	m.AcmeChallenge = optStr(l.AcmeChallenge)
	m.AcmeDNSProvider = optStr(l.AcmeDNSProvider)
	m.AcmeLastRenewalAt = optStr(l.AcmeLastRenewalAt)
	m.AcmeIssuedAt = optStr(l.AcmeIssuedAt)
	m.AcmeRenewAfter = optStr(l.AcmeRenewAfter)
	m.AcmeLastError = optStr(l.AcmeLastError)
	m.CertPath = optStr(l.CertPath)
	m.CreatedAt = types.StringValue(l.CreatedAt)

	// acme_dns_credentials is write-only — never returned by the API. Carry
	// over whatever is already in the model (the plan in Create, the prior
	// state in Read); never mark Null/Unknown, otherwise perma-diff.
	if m.AcmeDNSCredentials.IsUnknown() {
		m.AcmeDNSCredentials = types.MapNull(types.StringType)
	}
}

// validateDNS01 enforces the dns01 invariant: provider + credentials are
// required. Returns diagnostics rather than mutating the config. Early-returns
// when acme_challenge is unresolved so it never fires at `terraform validate`
// before defaults/plan modifiers run.
func validateDNS01(cfg listenerResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if cfg.AcmeChallenge.IsNull() || cfg.AcmeChallenge.IsUnknown() {
		return diags
	}
	if cfg.AcmeChallenge.ValueString() != "dns01" {
		return diags
	}
	if cfg.AcmeDNSProvider.IsNull() || cfg.AcmeDNSProvider.IsUnknown() || cfg.AcmeDNSProvider.ValueString() == "" {
		diags.AddAttributeError(
			path.Root("acme_dns_provider"),
			"acme_dns_provider required for dns01",
			"`acme_dns_provider` must be set when `acme_challenge = \"dns01\"`. "+
				"See the `ccp_acme_dns_providers` data source for the supported catalog.",
		)
	}
	if cfg.AcmeDNSCredentials.IsNull() || cfg.AcmeDNSCredentials.IsUnknown() || len(cfg.AcmeDNSCredentials.Elements()) == 0 {
		diags.AddAttributeError(
			path.Root("acme_dns_credentials"),
			"acme_dns_credentials required for dns01",
			"`acme_dns_credentials` must be set when `acme_challenge = \"dns01\"`.",
		)
	}
	return diags
}
