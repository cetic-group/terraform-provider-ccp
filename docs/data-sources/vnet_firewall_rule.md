---
page_title: "ccp_vnet_firewall_rule Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up a VNet firewall rule by (id, vnet_id).
---

# ccp_vnet_firewall_rule (Data Source)

Look up a VNet firewall rule by `(id, vnet_id)`. Both are required.

## Attributes Reference

- `id`, `vnet_id`, `position`, `direction`, `action`, `enabled`
- `proto`, `source_cidr`, `dest_cidr`, `dport`, `comment` (nullable)
- `created_at`
