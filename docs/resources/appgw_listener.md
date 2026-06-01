---
page_title: "ccp_appgw_listener Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a hostname + TLS cert listener on a CETIC Cloud Application Gateway.
---

# ccp_appgw_listener (Resource)

Manages a listener (hostname + TLS certificate) on a [`ccp_application_gateway`](application_gateway.md). Each listener gets its own Let's Encrypt certificate, served via SNI when the client requests this hostname.

For `custom_domain = true`, the client must already point a CNAME from `hostname` to the gateway's auto-generated subdomain **before** this resource is created — ACME DNS-01 validation will otherwise fail.

~> **`appgw_id`, `hostname` and `custom_domain` are immutable.** Any change forces a destroy + create.

## Example Usage

```hcl
resource "ccp_appgw_listener" "api" {
  appgw_id      = ccp_application_gateway.web.id
  hostname      = "api.example.com"
  custom_domain = true
}

resource "ccp_appgw_listener" "admin" {
  appgw_id = ccp_application_gateway.web.id
  hostname = "admin.example.com"
  # custom_domain defaults to false — listener serves an auto-generated
  # subdomain under app.cloud.cetic-group.com
}
```

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `hostname` - (Required, Forces new resource) Fully-qualified hostname served by this listener. Must be a valid RFC 1123 hostname (max 253 chars).

### Optional

- `custom_domain` - (Optional, default `false`, Forces new resource) When `true`, the hostname is a customer-owned domain (CNAME required, ACME validation uses DNS-01). When `false`, the listener serves an auto-generated subdomain under `app.cloud.cetic-group.com` and ACME uses HTTP-01.

## Attributes Reference

- `id` - UUID of the listener.
- `acme_status` - ACME issuance state: `pending`, `issued`, `failed`.
- `acme_last_renewal_at` - RFC 3339 timestamp of the last successful renewal.
- `cert_path` - Server-side filesystem path of the live certificate (informational).
- `created_at` - RFC 3339 creation timestamp.

## Import

```
terraform import ccp_appgw_listener.api <appgw_id>/<listener_id>
```
