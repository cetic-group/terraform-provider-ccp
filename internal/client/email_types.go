package client

// ─── Hosted email (EmaaS) — domains, mailboxes, aliases ──────────────────────
//
// Backend: `apps/api/app/api/v1/email_{domains,accounts,aliases}.py` (#932).
//
// Three shapes that do not show up in the resource schema:
//
//  1. A domain is born in `pending_verification`. Nothing is routed and no
//     mailbox can be created until the ownership TXT record is published and
//     `POST /{id}/verify` succeeds.
//  2. Quota is written in GIGABYTES (`quota_gb`) and read back in BYTES
//     (`quota_bytes`). The two are not the same field, and the read side is
//     the billing figure — reserved space, never used space.
//  3. Passwords are write-only. No response of the email API carries one, in
//     any shape: a mailbox password is reset, never read back.

// Email domain lifecycle states.
const (
	EmailDomainStatusPendingVerification = "pending_verification"
	EmailDomainStatusActive              = "active"
	EmailDomainStatusSuspended           = "suspended"
)

// EmailDomainDNSRecord is one DNS record the customer is expected to publish
// in the public zone of the domain, along with the state actually observed.
//
// `Value` is the canonical, complete line (a MX carries its priority).
// `Hostname` + `Priority` are an ADDITIONAL split for the DNS control panels
// that ask for two separate fields; they always come as a pair, and are nil
// for every other type.
type EmailDomainDNSRecord struct {
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Status   string  `json:"status"`
	Hostname *string `json:"hostname"`
	Priority *int64  `json:"priority"`
	// The published value is correct but the zone as a whole exceeds the SPF
	// lookup budget, so SPF fails for the whole domain. Orthogonal to Status.
	ExceedsLookupLimit bool   `json:"exceeds_lookup_limit"`
	Purpose            string `json:"purpose"`
}

// EmailClientEndpoint is one server to configure in a mail client.
type EmailClientEndpoint struct {
	Protocol string `json:"protocol"`
	Hostname string `json:"hostname"`
	Port     int64  `json:"port"`
	Security string `json:"security"`
}

// EmailClientConfig is the "settings to copy into your mail app" block.
type EmailClientConfig struct {
	Incoming     EmailClientEndpoint `json:"incoming"`
	Outgoing     EmailClientEndpoint `json:"outgoing"`
	UsernameHint string              `json:"username_hint"`
}

// EmailDomain is a hosted mail domain. `Verification`, `Records` and
// `ClientConfig` are only populated by the single-domain GET — the list shape
// omits them (CLAUDE.md pitfall #5).
type EmailDomain struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	VerifiedAt      *string `json:"verified_at"`
	DKIMGeneratedAt *string `json:"dkim_generated_at"`
	// True when the domain is driven from infrastructure code: the CCP console
	// then goes read-only on it. Set by the platform operator, never by this
	// provider — two control planes on one domain always diverge.
	ExternallyManaged bool   `json:"externally_managed"`
	CreatedAt         string `json:"created_at"`
	AccountsCount     *int64 `json:"accounts_count"`
	AliasesCount      *int64 `json:"aliases_count"`

	Verification *EmailDomainDNSRecord  `json:"verification"`
	Records      []EmailDomainDNSRecord `json:"records"`
	ClientConfig *EmailClientConfig     `json:"client_config"`
}

type EmailDomainCreateRequest struct {
	Name string `json:"name"`
}

// EmailAccount is a mailbox. No password field exists here by design.
type EmailAccount struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	// Reserved space, in bytes — the billing basis, not the space used.
	QuotaBytes      int64   `json:"quota_bytes"`
	UsageBytes      *int64  `json:"usage_bytes"`
	UsageUpdatedAt  *string `json:"usage_updated_at"`
	Enabled         bool    `json:"enabled"`
	EnableIMAP      bool    `json:"enable_imap"`
	EnablePOP       bool    `json:"enable_pop"`
	IsSystemManaged bool    `json:"is_system_managed"`
	// "Send as any address" — governed by its own API route and its own IAM
	// action, never by the general update. Read-only here.
	SendAsAnyAddress bool `json:"send_as_any_address"`
	SendAsPending    bool `json:"send_as_pending"`

	ForwardEnabled     bool     `json:"forward_enabled"`
	ForwardDestination []string `json:"forward_destination"`
	ForwardKeep        bool     `json:"forward_keep"`

	Comment       *string `json:"comment"`
	DisplayedName *string `json:"displayed_name"`
	CreatedAt     string  `json:"created_at"`

	ClientConfig *EmailClientConfig `json:"client_config"`
}

// EmailAccountCreateRequest — `QuotaGB` nil lets the platform apply its own
// configured default rather than freezing one in the provider.
type EmailAccountCreateRequest struct {
	Address       string  `json:"address"`
	Password      string  `json:"password"`
	QuotaGB       *int64  `json:"quota_gb,omitempty"`
	Comment       *string `json:"comment,omitempty"`
	DisplayedName *string `json:"displayed_name,omitempty"`
	EnableIMAP    *bool   `json:"enable_imap,omitempty"`
	EnablePOP     *bool   `json:"enable_pop,omitempty"`
}

// EmailAccountUpdateRequest is a partial update — every field is optional and
// omitted fields are left untouched. `address` is absent on purpose: renaming
// a mailbox would move already-delivered mail and break every forwarding rule
// pointing at it.
type EmailAccountUpdateRequest struct {
	QuotaGB            *int64    `json:"quota_gb,omitempty"`
	Enabled            *bool     `json:"enabled,omitempty"`
	Comment            *string   `json:"comment,omitempty"`
	DisplayedName      *string   `json:"displayed_name,omitempty"`
	EnableIMAP         *bool     `json:"enable_imap,omitempty"`
	EnablePOP          *bool     `json:"enable_pop,omitempty"`
	ForwardEnabled     *bool     `json:"forward_enabled,omitempty"`
	ForwardDestination *[]string `json:"forward_destination,omitempty"`
	ForwardKeep        *bool     `json:"forward_keep,omitempty"`
}

type EmailAccountPasswordResetRequest struct {
	Password string `json:"password"`
}

type EmailAlias struct {
	ID           string   `json:"id"`
	Address      string   `json:"address"`
	Destinations []string `json:"destinations"`
	Wildcard     bool     `json:"wildcard"`
	Comment      *string  `json:"comment"`
	CreatedAt    string   `json:"created_at"`
}

type EmailAliasCreateRequest struct {
	Address      string   `json:"address"`
	Destinations []string `json:"destinations"`
	Wildcard     bool     `json:"wildcard"`
	Comment      *string  `json:"comment,omitempty"`
}

type EmailAliasUpdateRequest struct {
	Destinations *[]string `json:"destinations,omitempty"`
	Wildcard     *bool     `json:"wildcard,omitempty"`
	Comment      *string   `json:"comment,omitempty"`
}
