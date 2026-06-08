---
page_title: "ccp_appgw_listener Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a hostname + optional Let's Encrypt TLS cert listener on a CETIC Cloud Application Gateway.
---

# ccp_appgw_listener (Resource)

Manages a listener (a hostname served by a [`ccp_application_gateway`](application_gateway.md), with an optional automatically-issued TLS certificate via Let's Encrypt/ACME). When `acme_challenge` is set, the certificate is requested automatically and served over SNI when a client connects with this hostname.

~> **All attributes are immutable.** Any change forces a destroy + create.

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
```

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `hostname` - (Required, Forces new resource) Fully-qualified, lowercase hostname served by this listener. Must be a valid RFC 1123 hostname (max 253 chars).

### Optional

- `acme_challenge` - (Optional, Forces new resource) ACME (Let's Encrypt) challenge type used to issue the listener's TLS certificate: `http01` or `dns01`. `dns01` additionally requires `acme_dns_provider` and `acme_dns_credentials`. **Without this attribute, no TLS certificate is ever issued for the listener.**
- `acme_dns_provider` - (Optional, Forces new resource) DNS provider key used for the `dns01` challenge (e.g. `cloudflare`, `route53`, `ionos`). See the [`ccp_acme_dns_providers`](../data-sources/acme_dns_providers.md) data source for the supported catalog. Required when `acme_challenge = "dns01"`. The `ionos` provider expects `prefix` and `secret` credentials.
- `acme_dns_credentials` - (Optional, Sensitive, Forces new resource) DNS provider credentials for the `dns01` challenge (write-only — never returned by the API). The expected keys depend on the provider (see `ccp_acme_dns_providers`). Required when `acme_challenge = "dns01"`.

## Attributes Reference

- `id` - UUID of the listener.
- `acme_status` - ACME issuance state: `pending`, `issued`, `failed`.
- `acme_last_renewal_at` - RFC 3339 timestamp of the last successful renewal.
- `acme_issued_at` - RFC 3339 timestamp at which the current certificate was issued.
- `acme_renew_after` - RFC 3339 timestamp after which the certificate is eligible for renewal.
- `acme_last_error` - Last ACME error message, if issuance or renewal failed.
- `cert_path` - Server-side filesystem path of the live certificate (informational).
- `created_at` - RFC 3339 creation timestamp.

## Import

```
terraform import ccp_appgw_listener.api <appgw_id>/<listener_id>
```

~> **Note:** `acme_dns_credentials` is write-only and is not recoverable on import. If the imported listener uses a DNS-01 certificate, declaring `acme_dns_credentials` in your configuration after import will propose a replacement of the listener.
