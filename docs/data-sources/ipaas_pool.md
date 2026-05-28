---
page_title: "ccp_ipaas_pool Data Source - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Look up an IPaaS pool by ID (admin only).
---

# ccp_ipaas_pool (Data Source)

Look up an IPaaS pool by `id`. Requires admin scope.

## Attributes Reference

- `id`, `region`, `cidr`, `gateway`, `kind`
- `edge_id` (nullable)
- `is_active`
- `created_at`
