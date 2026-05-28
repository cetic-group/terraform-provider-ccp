---
page_title: "ccp_vnet_peering Data Source - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Look up a VNet peering by ID.
---

# ccp_vnet_peering (Data Source)

Look up an existing VNet peering. Lookup is by `id` only — the API exposes no list endpoint.

## Example Usage

```hcl
data "ccp_vnet_peering" "core" {
  id = "11111111-2222-3333-4444-555555555555"
}
```

## Argument Reference

### Required

- `id` — UUID of the peering.

## Attributes Reference

- `id`, `name`
- `vnet_a_id`, `vnet_b_id` — UUIDs of the two peered VNets.
- `status` — Lifecycle status.
- `error_message` — Last error if any (nullable).
- `tags`
- `created_at` — RFC 3339 creation timestamp.
