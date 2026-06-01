---
page_title: "ccp_acme_dns_providers Data Source - ccp"
subcategory: "Networking"
description: |-
  Catalog of DNS providers supported for Let's Encrypt DNS-01 challenges.
---

# ccp_acme_dns_providers (Data Source)

Catalog of DNS providers supported for Let's Encrypt DNS-01 challenges (load balancer and application gateway listeners), with the credential field names each provider expects.

Use this to discover the valid `acme_dns_provider` keys and the `acme_dns_credentials` field names for a `ccp_appgw_listener` (or `ccp_load_balancer` listener) using the `dns01` challenge.

## Example Usage

```hcl
data "ccp_acme_dns_providers" "all" {}

output "cloudflare_fields" {
  value = data.ccp_acme_dns_providers.all.providers["cloudflare"].fields
}
```

## Argument Reference

This data source takes no arguments.

## Attributes Reference

- `providers` - Map of supported DNS providers keyed by provider id (e.g. `cloudflare`). Each value is an object:
  - `label` - Human-readable provider name.
  - `fields` - List of credential field names expected in `acme_dns_credentials` for this provider.
