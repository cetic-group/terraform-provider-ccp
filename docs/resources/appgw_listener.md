---
page_title: "ccp_appgw_listener Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a hostname + optional Let's Encrypt TLS cert listener on a CETIC Cloud Application Gateway.
---

# ccp_appgw_listener (Resource)

Manages a listener (a hostname served by a [`ccp_application_gateway`](application_gateway.md), with an optional automatically-issued TLS certificate via Let's Encrypt/ACME). When `acme_challenge` is set, the certificate is requested automatically and served over SNI when a client connects with this hostname.

~> Hostname and certificate acquisition settings are immutable. Access policies and custom certificate material can be updated in place.

-> **Without `acme_challenge`, no TLS certificate is ever issued for the listener.**

## Example Usage

```hcl
# HTTP-01 challenge — the gateway must be reachable on port 80 for the hostname.
resource "ccp_appgw_listener" "api" {
  appgw_id       = ccp_application_gateway.web.id
  hostname       = "api.example.com"
  acme_challenge = "http01"
}

# DNS-01 challenge — credentials for a supported DNS provider.
data "ccp_acme_dns_providers" "all" {}

resource "ccp_appgw_listener" "admin" {
  appgw_id          = ccp_application_gateway.web.id
  hostname          = "admin.example.com"
  acme_challenge    = "dns01"
  acme_dns_provider = "cloudflare"
  acme_dns_credentials = {
    api_token = var.cloudflare_api_token
  }
}

# Your own certificate — changing either file rotates it in place,
# without recreating the listener.
resource "ccp_appgw_listener" "shop" {
  appgw_id            = ccp_application_gateway.web.id
  hostname            = "shop.example.com"
  acme_challenge      = "custom"
  tls_certificate_pem = file("${path.module}/fullchain.pem")
  tls_private_key_pem = file("${path.module}/privkey.pem")
}
```

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `hostname` - (Required, Forces new resource) Fully-qualified, lowercase hostname served by this listener. Must be a valid RFC 1123 hostname (max 253 chars).

### Optional

- `acme_challenge` - (Optional, Forces new resource) How the listener's TLS certificate is obtained: `http01` or `dns01` (Let's Encrypt; `dns01` additionally requires `acme_dns_provider` and `acme_dns_credentials`), or `custom` (your own certificate, provided in `tls_certificate_pem` + `tls_private_key_pem`). **Without this attribute, no TLS certificate is ever issued for the listener.**
- `tls_certificate_pem` - (Optional, Sensitive) With `acme_challenge = "custom"`: your certificate followed by its chain, PEM-encoded. It must cover `hostname`; matching key and validity are checked at submission. **Changing it rotates the certificate in place** — the listener is not recreated, so traffic for the hostname is not cut. Never returned by the API.
- `tls_private_key_pem` - (Optional, Sensitive) With `acme_challenge = "custom"`: the private key, PEM-encoded and **not** protected by a passphrase. Write-only: the value you provide is kept in state. Changing it rotates the certificate in place.

`acme_challenge = "custom"` requires both `tls_certificate_pem` and `tls_private_key_pem`, and these two attributes require `acme_challenge = "custom"` (checked at plan time).
- `acme_dns_provider` - (Optional, Forces new resource) DNS provider key used for the `dns01` challenge (e.g. `cloudflare`, `route53`, `ionos`). See the [`ccp_acme_dns_providers`](../data-sources/acme_dns_providers.md) data source for the supported catalog. Required when `acme_challenge = "dns01"`. The `ionos` provider expects `prefix` and `secret` credentials.
- `acme_dns_credentials` - (Optional, Sensitive, Forces new resource) DNS provider credentials for the `dns01` challenge (write-only — never returned by the API). The expected keys depend on the provider (see `ccp_acme_dns_providers`). Required when `acme_challenge = "dns01"`.

## Attributes Reference

- `id` - UUID of the listener.
- `acme_status` - ACME issuance state: `pending`, `issued`, `failed`.
- `acme_last_renewal_at` - RFC 3339 timestamp of the last successful renewal.
- `acme_issued_at` - RFC 3339 timestamp at which the current certificate was issued.
- `acme_renew_after` - RFC 3339 timestamp after which the certificate is eligible for renewal.
- `acme_last_error` - Last ACME error message, if issuance or renewal failed.
- `cert_path` - Server-side path of the live certificate (informational).
- `tls_cert_not_after` - Expiry date of your certificate (`acme_challenge = "custom"`). A certificate you provide is **not** renewed automatically: rotate it before this date.
- `tls_cert_names` - Names covered by your certificate.
- `tls_cert_issuer` - Issuer of your certificate.
- `created_at` - RFC 3339 creation timestamp.

## Import

```
terraform import ccp_appgw_listener.api <appgw_id>/<listener_id>
```

~> **Note:** `acme_dns_credentials` is write-only and is not recoverable on import. If the imported listener uses a DNS-01 certificate, declaring `acme_dns_credentials` in your configuration after import will propose a replacement of the listener.

## HTTP access policies (provider 8.1+)

Requires provider `>= 8.1.0` once released and a platform advertising
`appgw_security_policy_versions = [1]`. This change does not publish a release.
The provider checks this capability before submitting a policy, including `{}`.
An older server cannot silently ignore the configuration. Nonempty documents
also require `appgw_security_policies_enabled = true`. Explicit `{}` resets
remain available when new policy activation is disabled.

- `l7_policy` - (Optional, Computed) HTTP access policies. Omit to preserve the
  remote policy, including after import. Set `{}` explicitly to clear the local
  policy. Removing the attribute relinquishes management without disabling
  remote protection. Changing this object updates the resource in place.
- `enabled` - (Optional, default `false`) Enable the policies in this object.
- `policies` - (Optional) Map of at most eight policies, keyed by stable names.
  Names begin with a lowercase letter and contain at most 32 lowercase letters,
  digits, underscores or hyphens.
- `scope` - (Optional) Independent protected request scope; omission covers all
  requests reaching this owner. A route policy also covers the route's path
  independently of its method/header routing selectors.
- `path` - (Optional, inside a scope or rule match) Path condition evaluated on
  the original request before rewrites. Ambiguous encodings are rejected.
- `op` - (Required for a path or header condition) Paths: `exact` or
  `segment_prefix` (`/admin` matches `/admin/users`, not `/administrator`).
  Headers: `present`, `absent`, `non_empty`, `eq`, `in`.
- `value` - (Required for a path condition) Path beginning with `/`.
- `allow_rules` - (Optional) Map of rules, combined with OR. If nonempty, at
  least one must match within the policy scope. Omission means no allow filter.
- `deny_rules` - (Optional) Map of rules, combined with OR; a matching deny wins.
- `match` - (Required in every rule) All supplied criteria combine with AND.
- `source_cidrs` - (Optional) Nonempty list of IPv4/IPv6 addresses or networks,
  at most 256 entries. Uses the gateway's `client_ip_policy` interpretation.
- `methods` - (Optional) Nonempty list of at most 16 uppercase HTTP methods.
- `headers` - (Optional) List of one to eight header conditions, combined with AND.
- `match_all` - (Optional) Explicit unconditional rule: only `true`, without any
  other criterion. It still applies only within the policy's independent scope.
- `required_headers` - (Optional) Map of at most eight mandatory header conditions,
  combined with AND independently of allow rules. A matching IP allow cannot
  bypass a required header. Keys must be unique across this policy's allow,
  deny and required-header maps.
- `name` - (Required in a header condition) Case-insensitive HTTP header name,
  at most 64 characters.
- `values` - (Optional in a header condition) Case-sensitive exact ASCII scalars,
  at most 16 values of 256 bytes. `eq` requires one; `in` requires one or more;
  presence operations accept none. Empty values, commas and surrounding
  whitespace are rejected. Examined duplicate headers are rejected too.
- `security_revision` - (Computed) Gateway-wide revision observed for this
  resource. The provider obtains the current revision for each policy write;
  users do not set it. Concurrent changes return a conflict: refresh and review
  the plan before applying again. Conflicts are not retried automatically.

The aggregate limit is 64 rules and header requirements across the gateway,
listeners and routes. Applicable policies intersect. A local allow cannot
bypass an inherited deny, legacy network restrictions or authentication.
Header checks are access filters, not a substitute for authentication; avoid
storing credentials in conditions. Terraform state contains policy values.

A hostname protected by any HTTP policy cannot contain regex routes. Header
conditions also require `cache_mode = "off"` on every overlapping route,
including when gateway caching is currently disabled. Disjoint scopes can
keep caching. Update each conflicting route and reapply the desired parent
policy; for a route's own policy, the cache change and policy can share a PATCH.
The API validates the whole gateway, including sibling resources.

Terraform waits for the accepted generation to be applied to every expected
instance, with old connections revoked, and verifies that managed policy fields
still match. A newer generation from an unrelated resource is accepted only
when these conditions remain true. An `active` gateway or accepted PATCH alone
is insufficient. Timeouts and conflicts keep the resource ID in state; refresh
and inspect the configuration before retrying. Deleting a child on a gateway
with tracked policies also waits for the committed deletion generation and
absence of that child.
