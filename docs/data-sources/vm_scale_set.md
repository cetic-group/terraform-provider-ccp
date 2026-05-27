---
page_title: "ccp_vm_scale_set Data Source - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Look up a VM scale set.
---

# ccp_vm_scale_set (Data Source)

Look up a VM scale set by `id` or `(name, region)`.

## Attributes Reference

- `id`, `name`, `region`, `plan`, `template`, `vnet_id` (nullable)
- `min_instances`, `max_instances`, `desired_instances`, `auto_repair`
- `status`, `error_message` (nullable)
- `tags`, `created_at`, `updated_at`
