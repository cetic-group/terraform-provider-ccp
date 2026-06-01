---
page_title: "ccp_application_gateway Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up an existing CETIC Cloud Application Gateway by id or by (name, region).
---

# ccp_application_gateway (Data Source)

Look up an existing CETIC Cloud Application Gateway by `id` or by `(name, region)`. Returns all gateway attributes plus a read-only summary of attached listeners, target groups and routes.

Exactly one of `id` OR (`name` + `region`) must be provided.

## Example Usage — by id

```hcl
data "ccp_application_gateway" "by_id" {
  id = "550e8400-e29b-41d4-a716-446655440000"
}

output "vip" {
  value = data.ccp_application_gateway.by_id.vip_address
}
```

## Example Usage — by name + region

```hcl
data "ccp_application_gateway" "by_name" {
  name   = "web-appgw"
  region = "RNN"
}

output "listeners" {
  value = data.ccp_application_gateway.by_name.listeners[*].hostname
}
```

## Argument Reference

- `id` - (Optional) UUID of the gateway. Conflicts with `name` + `region`.
- `name` - (Optional) Name of the gateway. Required together with `region`.
- `region` - (Optional) Region of the gateway. Required together with `name`.

## Attributes Reference

In addition to all arguments above:

- `plan`, `vpc_id`, `vnet_id`, `public_ip_id`, `vip_address`, `status`, `error_message`, `created_at`
- `force_https`, `hsts_enabled`, `hsts_max_age`
- `global_rate_limit_per_sec`, `global_allow_cidrs`, `global_deny_cidrs`, `trust_proxy_headers`
- `tags`
- `listeners` - List of `{id, hostname, acme_status, acme_challenge, acme_dns_provider, acme_last_renewal_at}`.
- `target_groups` - List of `{id, name, algorithm}`.
- `routes` - List of `{id, listener_id, priority, path_match, path_match_type, target_group_id}`.
