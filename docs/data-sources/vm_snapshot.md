---
page_title: "ccp_vm_snapshot Data Source - ccp"
subcategory: "Compute"
description: |-
  Look up a VM snapshot.
---

# ccp_vm_snapshot (Data Source)

Look up a VM snapshot by `(id, vm_instance_id)` or `(name, vm_instance_id)`.

## Attributes Reference

- `id`, `vm_instance_id`, `name`, `description` (nullable)
- `status`, `error_message` (nullable)
- `size_bytes` (nullable)
- `created_at`
