---
page_title: "ccp_vnet_ip_reservation Data Source - cetic-cloud-platform"
subcategory: "Network"
description: |-
  Look up a VNet IP reservation.
---

# ccp_vnet_ip_reservation (Data Source)

Look up a VNet IP reservation by `(id, vnet_id)` or `(name, vnet_id)`.

~> The Terraform attribute is `count_total` — the API's field is `count`, but `count` is a reserved Terraform identifier.

## Attributes Reference

- `id`, `vnet_id`, `name`, `ip`
- `range_end` (nullable), `description` (nullable)
- `count_total` — number of IPs reserved (1 for single, > 1 for range)
- `kind` — `single` or `range`
- `created_at`
