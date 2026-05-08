---
page_title: "ccp_vnet_peering Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a peering connection between two VNets on CETIC Cloud Platform.
---

# ccp_vnet_peering (Resource)

Manages an L3 peering connection between two VNets — same tenant, **either intra-VPC or inter-VPC**. Once active, instances on both sides can reach each other on private IPs without traversing the public internet.

~> **Note:** CETIC Cloud doesn't expose a "VPC peering" abstraction that would federate all VNets of two VPCs in one resource. To peer multiple VNet couples, declare one `ccp_vnet_peering` per couple.

~> **Order normalization:** the backend stores `vnet_a_id < vnet_b_id` (canonical order). Pass the UUIDs in any order — the provider normalizes.

## Example Usage

```hcl
# Peer two VNets that live in different VPCs
resource "ccp_vnet_peering" "data_to_web_cross_vpc" {
  name      = "prod-data-to-staging-web"
  vnet_a_id = ccp_vnet.prod_data.id
  vnet_b_id = ccp_vnet.staging_web.id
  tags      = ["env:prod", "purpose:cross-tier"]
}

# Intra-VPC peering between two VNets of the same VPC
resource "ccp_vnet_peering" "web_to_data" {
  name      = "web-to-data"
  vnet_a_id = ccp_vnet.web.id
  vnet_b_id = ccp_vnet.data.id
}
```

## Argument Reference

### Required

- `name` - Human-readable name for the peering (2-100 chars).
- `vnet_a_id` - UUID of one VNet. Forces replacement.
- `vnet_b_id` - UUID of the other VNet. Must be different from `vnet_a_id`. Forces replacement.

### Optional

- `tags` - List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

- `id` - UUID of the peering.
- `status` - One of `pending`, `active`, `deleting`, `error`.
- `created_at` - RFC3339 timestamp of creation.

## Import

```
terraform import ccp_vnet_peering.example <peering_id>
```
