---
page_title: "ccp_vnet_peering Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a peering connection between two VNets within a VPC on CETIC Cloud Platform.
---

# ccp_vnet_peering (Resource)

Manages an intra-VPC peering connection between two VNets that belong to the same VPC. Peered VNets route traffic directly through the shared NAT Gateway — no public internet traversal. The NAT Gateway automatically handles forwarding and avoids double-NATting between peered VNets.

~> **Note:** Intra-VPC routing (VNets in the same VPC) is handled automatically via the NAT Gateway when VNets are created. This resource is used when you need an explicit peering record for tracking or policy purposes. For inter-VPC peering (different VPCs), use `ccp_vpc_peering` instead.

## Example Usage

```hcl
resource "ccp_vnet_peering" "web_to_db" {
  vpc_id      = ccp_vpc.main.id
  peer_vpc_id = ccp_vpc.main.id
}
```

## Argument Reference

### Required

- `vpc_id` - (Required, Forces new resource) UUID of the source VPC.
- `peer_vpc_id` - (Required, Forces new resource) UUID of the peer VPC. For intra-VPC peering, this is the same as `vpc_id`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the peering connection.
- `status` - Current status. Possible values: `pending`, `active`, `error`.

## Import

VNet peering connections can be imported using their UUID:

```
terraform import ccp_vnet_peering.web_to_db <peering_id>
```
