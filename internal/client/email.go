package client

import (
	"context"
	"net/http"
	"net/url"
)

// ─── Email domains — /v1/email/domains ───────────────────────────────────────

// ListEmailDomains returns the domains of the calling organisation. The list
// shape omits `verification`, `records` and `client_config`.
func (c *Client) ListEmailDomains(ctx context.Context) ([]EmailDomain, error) {
	var out []EmailDomain
	if err := c.do(ctx, http.MethodGet, "/v1/email/domains", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateEmailDomain reserves the name. Nothing is routed yet: the domain is
// born in `pending_verification` and only VerifyEmailDomain activates it.
func (c *Client) CreateEmailDomain(ctx context.Context, req EmailDomainCreateRequest) (*EmailDomain, error) {
	var out EmailDomain
	if err := c.do(ctx, http.MethodPost, "/v1/email/domains", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetEmailDomain reads the full domain record: expected DNS records with their
// observed state, and the mail-client settings.
func (c *Client) GetEmailDomain(ctx context.Context, id string) (*EmailDomain, error) {
	var out EmailDomain
	if err := c.do(ctx, http.MethodGet, "/v1/email/domains/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyEmailDomain looks for the ownership TXT record and, when it is found,
// creates the domain on the mail server. Replayable: an already-active domain
// is returned as-is.
func (c *Client) VerifyEmailDomain(ctx context.Context, id string) (*EmailDomain, error) {
	var out EmailDomain
	if err := c.do(ctx, http.MethodPost, "/v1/email/domains/"+url.PathEscape(id)+"/verify", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecheckEmailDomain re-observes the DNS records of the zone and returns the
// refreshed record. Unlike the GET it is a write call server-side.
func (c *Client) RecheckEmailDomain(ctx context.Context, id string) (*EmailDomain, error) {
	var out EmailDomain
	if err := c.do(ctx, http.MethodPost, "/v1/email/domains/"+url.PathEscape(id)+"/recheck", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteEmailDomain removes the domain. Refused (409) while it still carries
// mailboxes or aliases — deleting is not a cascade, it would destroy mail.
func (c *Client) DeleteEmailDomain(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/email/domains/"+url.PathEscape(id), nil, nil)
}

// ─── Mailboxes — /v1/email/accounts ──────────────────────────────────────────

// ListEmailAccounts lists the mailboxes of the organisation, optionally
// restricted to one domain. Pass an empty domainID for all of them.
func (c *Client) ListEmailAccounts(ctx context.Context, domainID string) ([]EmailAccount, error) {
	path := "/v1/email/accounts"
	if domainID != "" {
		path += "?domain_id=" + url.QueryEscape(domainID)
	}
	var out []EmailAccount
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateEmailAccount creates a mailbox on an ACTIVE domain of the
// organisation. The domain is derived from the address.
func (c *Client) CreateEmailAccount(ctx context.Context, req EmailAccountCreateRequest) (*EmailAccount, error) {
	var out EmailAccount
	if err := c.do(ctx, http.MethodPost, "/v1/email/accounts", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEmailAccount(ctx context.Context, id string) (*EmailAccount, error) {
	var out EmailAccount
	if err := c.do(ctx, http.MethodGet, "/v1/email/accounts/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateEmailAccount(ctx context.Context, id string, req EmailAccountUpdateRequest) (*EmailAccount, error) {
	var out EmailAccount
	if err := c.do(ctx, http.MethodPatch, "/v1/email/accounts/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetEmailAccountPassword replaces the mailbox password. Answers 204: the
// platform never holds the password, so there is nothing to return.
func (c *Client) ResetEmailAccountPassword(ctx context.Context, id, password string) error {
	body := EmailAccountPasswordResetRequest{Password: password}
	return c.do(ctx, http.MethodPost, "/v1/email/accounts/"+url.PathEscape(id)+"/password", body, nil)
}

// DeleteEmailAccount removes the mailbox AND its content. Irreversible.
func (c *Client) DeleteEmailAccount(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/email/accounts/"+url.PathEscape(id), nil, nil)
}

// ─── Aliases — /v1/email/aliases ─────────────────────────────────────────────

// ListEmailAliases lists the aliases of the organisation, optionally
// restricted to one domain.
func (c *Client) ListEmailAliases(ctx context.Context, domainID string) ([]EmailAlias, error) {
	path := "/v1/email/aliases"
	if domainID != "" {
		path += "?domain_id=" + url.QueryEscape(domainID)
	}
	var out []EmailAlias
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateEmailAlias(ctx context.Context, req EmailAliasCreateRequest) (*EmailAlias, error) {
	var out EmailAlias
	if err := c.do(ctx, http.MethodPost, "/v1/email/aliases", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetEmailAlias(ctx context.Context, id string) (*EmailAlias, error) {
	var out EmailAlias
	if err := c.do(ctx, http.MethodGet, "/v1/email/aliases/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateEmailAlias(ctx context.Context, id string, req EmailAliasUpdateRequest) (*EmailAlias, error) {
	var out EmailAlias
	if err := c.do(ctx, http.MethodPatch, "/v1/email/aliases/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteEmailAlias(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/email/aliases/"+url.PathEscape(id), nil, nil)
}
