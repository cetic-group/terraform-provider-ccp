---
page_title: "ccp_vpc Data Source - ccp"
subcategory: "Networking"
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
- `cidr` — Private address block of the VPC (RFC1918, e.g. `10.1.0.0/16`; `null` on legacy VPCs predating the field).
- `status` — `active`, `deleting`, or `error`.
- `tags` — Free-form tags.
- `created_at` — RFC 3339 creation timestamp.
- `dedicated_egress_ip` — `true` when the VPC has its own dedicated egress IP, `false` when it uses the shared egress of the platform (see [Dedicated egress IP](../resources/vpc.md#dedicated-egress-ip)).
- `egress_ip` — Dedicated egress public IP of the VPC (`null` while `dedicated_egress_ip` is `false`).
- `egress_ip_type` — **deprecated**, always `null` (the platform no longer reports the kind of the dedicated egress IP). Will be removed in the next major version.
- `egress_ip_status` — `allocated`, `attaching`, `attached`, `detaching` or `error` (`null` without dedicated egress IP).
