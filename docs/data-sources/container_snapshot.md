---
page_title: "ccp_container_snapshot Data Source - ccp"
subcategory: "Compute"
description: |-
  Look up a container snapshot.
---

# ccp_container_snapshot (Data Source)

Look up a container snapshot by `(id, container_id)` or `(name, container_id)`.

## Attributes Reference

- `id`, `container_id`, `name`, `description` (nullable)
- `status`, `error_message` (nullable)
- `size_bytes` (nullable)
- `created_at`
