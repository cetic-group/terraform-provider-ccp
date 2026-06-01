---
page_title: "ccp_vnet Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up an existing VNet inside a VPC by (id, vpc_id) or (name, vpc_id).
---

# ccp_vnet (Data Source)

Look up an existing VNet within a VPC. `vpc_id` is always required.

## Example Usage

```hcl
data "ccp_vpc" "main" {
  name   = "prod"
  region = "RNN"
}

# By name within the VPC
data "ccp_vnet" "frontend" {
  vpc_id = data.ccp_vpc.main.id
  name   = "frontend"
}

# By ID
data "ccp_vnet" "by_id" {
  vpc_id = data.ccp_vpc.main.id
  id     = "11111111-2222-3333-4444-555555555555"
}
```

## Argument Reference

### Required

- `vpc_id` — UUID of the parent VPC.

### Optional (exactly one)

- `id` — UUID of the VNet.
- `name` — Name of the VNet within the VPC.

## Attributes Reference

- `id`, `vpc_id`, `name`
- `cidr` — IPv4 CIDR block.
- `gateway` — Gateway IP address (nullable).
- `dhcp_start`, `dhcp_end` — DHCP range bounds (nullable).
- `snat` — Whether outbound SNAT is enabled.
- `isolated` — Whether the VNet is isolated (no L3 routing).
- `status` — `active`, `deleting`, or `error`.
- `tags`
- `created_at` — RFC 3339 creation timestamp.
