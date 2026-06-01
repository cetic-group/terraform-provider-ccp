---
page_title: "ccp_vm_instance Data Source - ccp"
subcategory: "Compute"
description: |-
  Look up a VM instance by ID or by (name, region).
---

# ccp_vm_instance (Data Source)

Look up a QEMU VM instance.

## Example Usage

```hcl
data "ccp_vm_instance" "app" {
  name   = "app-01"
  region = "RNN"
}
```

## Argument Reference

Provide **either** `id`, **or** `(name, region)`.

### Optional

- `id`, `name`, `region`

## Attributes Reference

- `id`, `name`, `region`
- `plan`, `cores`, `memory_mb`, `disk_gb`, `template`
- `status` — `provisioning`, `running`, `stopped`, `error`, `deleting`
- `ip_address`, `public_ip_address` (nullable)
- `vnet_id`, `scale_set_id` (nullable)
- `user_data` (nullable), `error_message` (nullable)
- `has_root_password`
- `tags`, `created_at`, `updated_at`
