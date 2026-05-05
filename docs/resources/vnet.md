---
page_title: "ccp_vnet Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a VNet (subnet) inside a VPC on CETIC Cloud Platform.
---

# ccp_vnet (Resource)

Manages a VNet (Virtual Network / subnet) inside a VPC. Each VNet maps to a VXLAN SDN network with a DHCP range managed by Proxmox IPAM. When the first VNet is created in a VPC, a NAT Gateway LXC is automatically provisioned to provide internet access for all instances via SNAT. Additional VNets in the same VPC are automatically routed through the same NAT Gateway — no peering configuration required.

~> **Note:** VNet creation is asynchronous — the provider polls until the VNet reaches `active` status. The first VNet in a VPC triggers NAT Gateway provisioning, which may take up to 2 minutes.

## Example Usage

```hcl
resource "ccp_vnet" "web" {
  vpc_id = ccp_vpc.main.id
  name   = "web-tier"
  cidr   = "10.0.1.0/24"
  snat   = true
  tags   = ["web", "env:prod"]
}

resource "ccp_vnet" "db" {
  vpc_id = ccp_vpc.main.id
  name   = "db-tier"
  cidr   = "10.0.2.0/24"
  snat   = true
  tags   = ["db", "env:prod"]
}
```

## Argument Reference

### Required

- `vpc_id` - (Required, Forces new resource) UUID of the parent VPC.
- `name` - (Required) Name of the VNet.
- `cidr` - (Required, Forces new resource) CIDR block for the VNet (e.g. `10.0.1.0/24`). Must not overlap with other VNets in the same VPC.

### Optional

- `snat` - (Optional) Whether outbound SNAT/masquerade is enabled for internet access. Defaults to `false`.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VNet.
- `status` - Current status of the VNet. Possible values: `pending`, `active`, `error`.
- `gateway` - IP address of the NAT Gateway on this VNet (the `.1` address of the CIDR). Available once the NAT Gateway is provisioned.

## Import

VNets can be imported using their UUID:

```
terraform import ccp_vnet.web <vnet_id>
```
