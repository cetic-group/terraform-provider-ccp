---
page_title: "ccp_vpc Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a VPC (Virtual Private Cloud) on CETIC Cloud Platform.
---

# ccp_vpc (Resource)

Manages a VPC (Virtual Private Cloud) on CETIC Cloud Platform. Each VPC creates an isolated private network in the target region. A NAT Gateway is automatically provisioned when the first VNet is created inside the VPC, providing internet access for all instances without exposing private IPs.

~> **Note:** VPC creation is asynchronous — the provider polls until the VPC reaches `active` status. Provisioning typically completes within 30 seconds.

## Example Usage

```hcl
resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
  cidr   = "10.1.0.0/16"
  tags   = ["env:prod", "team:infra"]
}

resource "ccp_vnet" "web" {
  vpc_id = ccp_vpc.main.id
  name   = "web-tier"
  cidr   = "10.1.1.0/24"
  snat   = true
}

resource "ccp_vnet" "data" {
  vpc_id = ccp_vpc.main.id
  name   = "data-tier"
  cidr   = "10.1.2.0/24"
  snat   = true
}
```

If `cidr` is omitted, the platform auto-allocates a `/16` private block; the
chosen block is then exported as a computed attribute.

### Internet egress IP

As soon as a VNet of the VPC has `snat = true`, the platform assigns the VPC
its own public egress IP: the outbound internet traffic of its `snat = true`
VNets leaves through it, so partners that filter by source address can allow
this single IP. There is nothing to configure: the IP is not an argument of
`ccp_vpc`.

```hcl
resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
}

resource "ccp_vnet" "web" {
  vpc_id = ccp_vpc.main.id
  name   = "web-tier"
  cidr   = "10.1.1.0/24"
  snat   = true
}

output "egress_ip" {
  value = ccp_vpc.main.egress_ip
}
```

-> **Note:** `egress_ip` shows up on `ccp_vpc` at the refresh that follows the
creation of the first `ccp_vnet` with `snat = true` (the next `terraform plan`,
`apply` or `refresh`), not in the apply that creates the VNet. The provider
does not wait for it.

The IP stays the same as long as at least one VNet of the VPC has
`snat = true`. When the last one goes away (VNet deleted or switched to
`snat = false`, VPC deleted), the platform removes it and gives it back: the
`egress_*` attributes become `null`. A new `snat = true` VNet gets a new IP.
It does not count against the `max_public_ips` quota, and it is read-only
through the public IP API (it cannot be renamed, attached, detached or
released).
If no public IP is free in the region, creating a `snat = true` VNet (or
switching one to `snat = true`) fails with HTTP 409 and nothing is created.
Internet egress is reserved for EVPN VPCs.

## Argument Reference

### Required

- `name` - (Required) Name of the VPC. Must be unique within the region.
- `region` - (Required, Forces new resource) Region where the VPC is created. One of: `RNN` (Rennes), `PAR` (Paris), `ABJ` (Abidjan).

### Optional

- `cidr` - (Optional, Computed, Forces new resource) Private address block of the VPC (RFC1918, mask `/16` to `/24`, e.g. `10.1.0.0/16`). Auto-allocated by the platform when omitted. VNets created inside the VPC must be sub-ranges of this block. Immutable after creation — changing it forces replacement.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each). Example: `["env:prod", "team:infra"]`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VPC.
- `cidr` - The private address block of the VPC (the auto-allocated block when not set explicitly; `null` on legacy VPCs predating the field).
- `status` - Current status of the VPC. Possible values: `pending`, `active`, `error`.
- `egress_ip` - Internet egress public IP of the VPC, assigned by the platform. `null` while no VNet of the VPC has `snat = true`. See [Internet egress IP](#internet-egress-ip).
- `egress_ip_type` - Kind of the egress IP: `edge`, `firewall` or `nat`. `null` without egress IP.
- `egress_ip_status` - Status of the egress IP: `allocated` (reserved, not yet in service), `attaching`, `attached` (in service), `detaching` or `error`. `null` without egress IP.
- `created_at` - Timestamp of creation (RFC3339).

## Import

VPCs can be imported using their UUID:

```
terraform import ccp_vpc.main <vpc_id>
```
