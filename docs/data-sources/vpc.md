---
page_title: "ccp_vpc Data Source - cetic-cloud-platform"
subcategory: "Network"
description: |-
  Look up an existing VPC by ID or by (name, region).
---

# ccp_vpc (Data Source)

Look up an existing VPC by `id`, or by the unique `(name, region)` pair.

## Example Usage

```hcl
# By ID
data "ccp_vpc" "main" {
  id = "11111111-2222-3333-4444-555555555555"
}

# By name + region
data "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

output "vpc_id" {
  value = data.ccp_vpc.prod.id
}
```

## Argument Reference

Provide **either** `id`, **or** the pair `(name, region)`. Combining the two yields an error.

### Optional

- `id` — UUID of the VPC.
- `name` — Name of the VPC. Combine with `region`.
- `region` — Region of the VPC. Combine with `name`.

## Attributes Reference

- `id` — UUID of the VPC.
- `name` — Name of the VPC.
- `region` — Region code.
- `vlan_id` — VLAN ID assigned by Proxmox SDN.
- `sdn_type` — SDN driver type (e.g. `evpn`).
- `status` — `active`, `deleting`, or `error`.
- `tags` — Free-form tags.
- `created_at` — RFC 3339 creation timestamp.
