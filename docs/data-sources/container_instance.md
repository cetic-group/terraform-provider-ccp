---
page_title: "ccp_container_instance Data Source - ccp"
subcategory: "Compute"
description: |-
  Look up an LXC container instance by ID or by (name, region).
---

# ccp_container_instance (Data Source)

Look up an LXC container instance.

## Example Usage

```hcl
data "ccp_container_instance" "edge" {
  name   = "edge-01"
  region = "RNN"
}
```

## Argument Reference

Provide **either** `id`, **or** `(name, region)`.

## Attributes Reference

- `id`, `name`, `region`
- `plan`, `cores`, `memory_mb`, `disk_gb`, `template`
- `status` — `provisioning`, `running`, `stopped`, `error`, `deleting`
- `ip_address`, `public_ip_address` (nullable)
- `vnet_id`, `scale_set_id` (nullable)
- `user_data` (nullable), `error_message` (nullable)
- `has_root_password`
- `tags`, `created_at`, `updated_at`
