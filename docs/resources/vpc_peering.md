---
page_title: "ccp_vpc_peering Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a peering connection between two VPCs on CETIC Cloud Platform.
---

# ccp_vpc_peering (Resource)

Manages an inter-VPC peering connection between two VPCs in the same region. Once established, instances in either VPC can route traffic to each other using private IPs — no public internet traversal. Routes and forwarding rules are automatically configured on both NAT Gateways.

~> **Note:** VPC peering is not supported across regions. Both VPCs must be in the same region. Peering will fail if the CIDRs of the two VPCs overlap. For intra-tenant peering, the connection is activated automatically. For cross-tenant peering, the accepter must approve the invitation separately via the console or CLI.

## Example Usage

```hcl
# Peer a production VPC with a staging VPC (same tenant, auto-accepted)
resource "ccp_vpc_peering" "prod_to_staging" {
  vpc_id          = ccp_vpc.production.id
  accepter_vpc_id = ccp_vpc.staging.id
}
```

## Argument Reference

### Required

- `vpc_id` - (Required, Forces new resource) UUID of the requester VPC.
- `accepter_vpc_id` - (Required, Forces new resource) UUID of the accepter VPC. Must be in the same region as the requester VPC. CIDRs must not overlap.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VPC peering connection.
- `status` - Current status. Possible values: `pending`, `active`, `error`.

## Import

VPC peering connections can be imported using their UUID:

```
terraform import ccp_vpc_peering.prod_to_staging <peering_id>
```
