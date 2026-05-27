---
page_title: "ccp_custom_template Data Source - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Look up a tenant-owned custom template.
---

# ccp_custom_template (Data Source)

Look up a custom template (snapshot promoted to a reusable image) by `id` or `name`.

## Attributes Reference

- `id`, `name`, `description` (nullable)
- `template_type` — `vm` or `container`
- `region`, `status`, `error_message` (nullable)
- `disk_gb` (nullable)
- `source_instance_id`, `source_instance_type` (nullable)
- `created_at`, `updated_at`
