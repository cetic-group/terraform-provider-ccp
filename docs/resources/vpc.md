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

### Dedicated egress IP

By default, the `snat = true` VNets of a VPC reach the internet through the
platform's shared egress. With `dedicated_egress_ip = true`, the platform picks
a public IP for the VPC and its outbound traffic leaves through it, so partners
that filter by source address can allow that single IP.

```hcl
resource "ccp_vpc" "main" {
  name                = "production"
  region              = "RNN"
  dedicated_egress_ip = true
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

-> **Note:** The IP is reserved as soon as the VPC is created
(`egress_ip_status = "allocated"`), and served (`attached`) once the VPC has a
`snat = true` VNet. A later `terraform refresh` or `plan` shows the new status.

The attribute is updated in place, without replacing the VPC. When it is
enabled, the provider waits (up to 10 minutes) for the IP to be `attached`, or
`allocated` if the VPC has no `snat = true` VNet yet. When it is set back to
`false` (or removed from the configuration), the provider waits until the
platform has proven the IP is no longer used: until then the `egress_*`
attributes stay filled with the `detaching` status, then they become `null`.
An `error` status fails the update; at creation it is a warning, so the VPC is
not replaced, and the platform retries on its own.

The IP counts against the `max_public_ips` quota. The platform refuses the
request with HTTP 422 when the feature is not offered or the VPC is not an EVPN
VPC, with HTTP 409 when the VPC is not active, no public IP is free, or a
previous removal is still running, and with HTTP 429 when the quota is reached.

## Argument Reference

### Required

- `name` - (Required) Name of the VPC. Must be unique within the region.
- `region` - (Required, Forces new resource) Region where the VPC is created. One of: `RNN` (Rennes), `PAR` (Paris), `ABJ` (Abidjan).

### Optional

- `cidr` - (Optional, Computed, Forces new resource) Private address block of the VPC (RFC1918, mask `/16` to `/24`, e.g. `10.1.0.0/16`). Auto-allocated by the platform when omitted. VNets created inside the VPC must be sub-ranges of this block. Immutable after creation — changing it forces replacement.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each). Example: `["env:prod", "team:infra"]`.
- `dedicated_egress_ip` - (Optional) Route the internet traffic of the VPC's `snat = true` VNets through a dedicated public IP picked by the platform. Defaults to `false`. Updated in place. See [Dedicated egress IP](#dedicated-egress-ip).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VPC.
- `cidr` - The private address block of the VPC (the auto-allocated block when not set explicitly; `null` on legacy VPCs predating the field).
- `status` - Current status of the VPC. Possible values: `pending`, `active`, `error`.
- `created_at` - Timestamp of creation (RFC3339).
- `egress_ip` - Dedicated egress public IP of the VPC. `null` while no IP is linked.
- `egress_ip_type` - Kind of the dedicated egress IP: `edge`, `firewall` or `nat`. `null` while no IP is linked.
- `egress_ip_status` - Status of the dedicated egress IP: `allocated` (reserved, served once the VPC has a `snat = true` VNet), `attaching`, `attached` (in service), `detaching` or `error`. `null` while no IP is linked.

## Import

VPCs can be imported using their UUID:

```
terraform import ccp_vpc.main <vpc_id>
```
