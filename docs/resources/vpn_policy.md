---
page_title: "ccp_vpn_policy Resource - ccp"
subcategory: "Networking"
description: |-
  Manages the access policy of a ccp_vpn_gateway.
---

# ccp_vpn_policy (Resource)

Manages the **access policy** of a [`ccp_vpn_gateway`](vpn_gateway.md). A policy is a **singleton per gateway** (one policy per gateway, keyed by `gateway_id`). It does two things:

- `groups` — assigns each peer client name to one or more **logical groups**.
- `rules` — an ordered list of access rules, each allowing a logical group to reach a destination CIDR on a set of ports and a protocol.

With no policy configured, the gateway grants peers **full access** to the VPCs it fronts. Defining a policy switches the gateway to a deny-by-default posture gated by the rules below. Clearing the policy — destroying this resource, or setting empty `groups` and `rules` — returns the gateway to full access.

~> **Note (ADMIN required):** Replacing the policy (`PUT`) requires an API token with the **ADMIN** role. A non-admin token will get a `403`.

~> **Note:** `gateway_id` is immutable — changing it forces replacement. `groups` and `rules` are mutable in place. The gateway returns the policy unchanged on read; if the gateway is deleted out-of-band, the policy is removed from state on the next refresh.

## Example Usage

```hcl
resource "ccp_vpn_gateway" "ops" {
  name    = "ops-vpn"
  region  = "RNN"
  plan    = "small"
  vpc_ids = [ccp_vpc.prod.id]
}

resource "ccp_vpn_policy" "ops" {
  gateway_id = ccp_vpn_gateway.ops.id

  # Peer client name => logical groups it belongs to
  groups = {
    "alice-laptop"  = ["admins"]
    "branch-router" = ["sites"]
  }

  # Admins may SSH and hit the API tier; sites only reach the app subnet over HTTPS
  rules = [
    {
      from_group = "admins"
      to_cidr    = "10.0.0.0/16"
      ports      = [22, 443]
      proto      = "tcp"
    },
    {
      from_group = "sites"
      to_cidr    = "10.0.10.0/24"
      ports      = [443]
      # proto defaults to "tcp"
    },
  ]
}
```

## Argument Reference

### Required

- `gateway_id` - (Required, Forces new resource) UUID of the `ccp_vpn_gateway` this policy governs.
- `groups` - (Required) Map of peer client name → list of logical group names that client belongs to. May be an empty map.
- `rules` - (Required) Ordered list of access rules (see below). May be an empty list.

### Nested `rules` block

Each element of `rules` supports:

- `from_group` - (Required) Logical group (a value used in `groups`) the rule applies to.
- `to_cidr` - (Required) Destination CIDR the group is allowed to reach.
- `ports` - (Optional) List of destination port numbers. Omit (or leave empty) for all ports.
- `proto` - (Optional) Protocol: `tcp` (default), `udp`, or `any`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - Resource identifier. A policy has no id of its own, so this mirrors `gateway_id`.

## Import

A VPN policy can be imported using the gateway UUID:

```
terraform import ccp_vpn_policy.ops <gateway_id>
```
