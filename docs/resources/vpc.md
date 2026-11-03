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

By default (`dedicated_egress_ip = false`), the `snat = true` VNets of the VPC
reach the internet through the **shared egress** of the platform: their
traffic leaves with a shared address, and outbound **TCP port 25** (direct mail
sending) is blocked there (ports 465 and 587 stay open). The VPC has no IP of
its own and the `egress_*` attributes are `null`.

With `dedicated_egress_ip = true`, the platform assigns the VPC **its own
public egress IP**: the outbound traffic of its `snat = true` VNets leaves
through it, so partners that filter by source address (payments, SMS, banking
APIs) can allow this single IP, and port 25 is not blocked. The IP is assigned
in the same apply, with or without a `snat = true` VNet, and **kept as long as
the option stays on**.

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

`egress_ip` is known at the end of the apply that turns the option on (at
creation or later). `egress_ip_status` is then `allocated` (reserved: no
`snat = true` VNet yet, or gateway not ready) or `attaching`, and becomes
`attached` once the IP is in service; the provider does not wait for it, the
next refresh shows it.

The option is changed **in place** (no replacement of the VPC). If no public
IP is free in the region, turning it on fails with HTTP 409; if the
installation does not offer it, or the VPC is not an EVPN VPC, with HTTP 422.
In both cases nothing is changed (or created) and the VPC keeps the shared
egress.

~> **Warning:** setting `dedicated_egress_ip` back to `false` — **or removing
the attribute from the configuration**, since it defaults to `false` — gives
the IP back to the pool **immediately**, with no grace period. It is no longer
reserved and may be assigned to another customer: warn the partners that allow
it first. Turning the option on again gives a **different** IP. Deleting the
VPC has the same effect. If the option was turned on outside Terraform (console,
API) or the VPC was imported, add `dedicated_egress_ip = true` to the
configuration before the next apply, or the plan will turn it off.

The dedicated IP is **billed as a public IP**, at the current rates, but does
not count against the `max_public_ips` quota. It is read-only through the
public IP API while it is bound to the VPC (it cannot be renamed, attached,
detached or released): to give it back, turn the option off. Outbound
connections already established are reset when the IP goes live or is
removed.

## Argument Reference

### Required

- `name` - (Required) Name of the VPC. Must be unique within the region.
- `region` - (Required, Forces new resource) Region where the VPC is created. One of: `RNN` (Rennes), `PAR` (Paris), `ABJ` (Abidjan).

### Optional

- `cidr` - (Optional, Computed, Forces new resource) Private address block of the VPC (RFC1918, mask `/16` to `/24`, e.g. `10.1.0.0/16`). Auto-allocated by the platform when omitted. VNets created inside the VPC must be sub-ranges of this block. Immutable after creation — changing it forces replacement.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each). Example: `["env:prod", "team:infra"]`.
- `dedicated_egress_ip` - (Optional, Computed, updated in place) Give the VPC its own dedicated public egress IP. Defaults to `false` (shared egress, outbound TCP port 25 blocked). Setting it back to `false`, or removing it, **releases the IP immediately**. Billed as a public IP. EVPN VPCs only. See [Dedicated egress IP](#dedicated-egress-ip).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VPC.
- `cidr` - The private address block of the VPC (the auto-allocated block when not set explicitly; `null` on legacy VPCs predating the field).
- `status` - Current status of the VPC. Possible values: `pending`, `active`, `error`.
- `egress_ip` - Dedicated egress public IP of the VPC. `null` while `dedicated_egress_ip` is `false` (shared egress). See [Dedicated egress IP](#dedicated-egress-ip).
- `egress_ip_type` - Kind of the dedicated egress IP: `edge`, `firewall` or `nat`. `null` without dedicated egress IP.
- `egress_ip_status` - Status of the dedicated egress IP: `allocated` (reserved, not yet in service), `attaching`, `attached` (in service), `detaching` (going back to the pool after the option was turned off) or `error`. `null` without dedicated egress IP.
- `created_at` - Timestamp of creation (RFC3339).

## Import

VPCs can be imported using their UUID:

```
terraform import ccp_vpc.main <vpc_id>
```
