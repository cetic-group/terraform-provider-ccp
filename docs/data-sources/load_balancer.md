---
page_title: "ccp_load_balancer Data Source - cetic-cloud-platform"
subcategory: "Network"
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

- `id`, `name`, `region`, `plan`, `vnet_id`
- `vip_address`, `public_ip_address`, `public_ip_id` (nullable)
- `status`, `error_message` (nullable)
- `tags`, `created_at`, `updated_at`
