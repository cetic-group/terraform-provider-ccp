---
page_title: "ccp_load_balancer Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up a Load Balancer by ID or by (name, region).
---

# ccp_load_balancer (Data Source)

Look up a Load Balancer. Listeners and backends are not surfaced — they are managed separately.

## Example Usage

```hcl
data "ccp_load_balancer" "main" {
  name   = "main"
  region = "RNN"
}
```

## Attributes Reference

- `id`, `name`, `region`, `plan`, `vnet_id`, `vpc_id`, `vpc_name`, `vnet_name`, `vnet_cidr`
- `vip_address`, `public_ip_address`, `public_ip_id` (nullable)
- `status`, `error_message` (nullable)
- `tags`, `created_at`, `updated_at`

### Availability

`replica_count` (Number, read-only) is `1` without redundancy or `2` for high
availability. A platform version predating this field only supports two
instances; its response is read as `2`.
