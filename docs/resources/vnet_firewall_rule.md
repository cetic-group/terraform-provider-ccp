---
page_title: "ccp_vnet_firewall_rule Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a firewall rule for a VNet on CETIC Cloud Platform.
---

# ccp_vnet_firewall_rule (Resource)

Manages a firewall rule for a VNet. Rules are applied per-instance on all compute resources attached to the VNet when the VNet's isolation mode is enabled. Rules evaluate in ascending `position` order — the first matching rule wins.

~> **Note:** Firewall rules are only enforced when the parent VNet has isolation enabled. Rules are immutable except for `enabled`, `position`, and `comment`. Changing any immutable field (`vnet_id`, `direction`, `action`, `proto`, `src_cidr`, `dst_cidr`, `dport`) forces a new resource.

## Example Usage

```hcl
# Allow inbound HTTP and HTTPS from the public internet
resource "ccp_vnet_firewall_rule" "allow_http" {
  vnet_id   = ccp_vnet.web.id
  direction = "IN"
  action    = "ACCEPT"
  proto     = "tcp"
  src_cidr  = "0.0.0.0/0"
  dport     = "80"
  enabled   = true
  position  = 10
  comment   = "Allow inbound HTTP"
}

resource "ccp_vnet_firewall_rule" "allow_https" {
  vnet_id   = ccp_vnet.web.id
  direction = "IN"
  action    = "ACCEPT"
  proto     = "tcp"
  src_cidr  = "0.0.0.0/0"
  dport     = "443"
  enabled   = true
  position  = 20
  comment   = "Allow inbound HTTPS"
}

# Allow SSH from the operations subnet only
resource "ccp_vnet_firewall_rule" "allow_ssh_ops" {
  vnet_id   = ccp_vnet.web.id
  direction = "IN"
  action    = "ACCEPT"
  proto     = "tcp"
  src_cidr  = "10.0.99.0/24"
  dport     = "22"
  enabled   = true
  position  = 5
  comment   = "SSH from ops VNet only"
}
```

## Argument Reference

### Required

- `vnet_id` - (Required, Forces new resource) UUID of the VNet this rule belongs to.
- `direction` - (Required, Forces new resource) Traffic direction. One of: `IN` (inbound), `OUT` (outbound).
- `action` - (Required, Forces new resource) Rule action. One of: `ACCEPT`, `DROP`.
- `proto` - (Required, Forces new resource) Protocol to match. One of: `tcp`, `udp`, `icmp`, `any`.

### Optional

- `src_cidr` - (Optional, Forces new resource) Source CIDR to match (e.g. `10.0.0.0/8`). Defaults to `0.0.0.0/0` (all sources).
- `dst_cidr` - (Optional, Forces new resource) Destination CIDR to match. Defaults to `0.0.0.0/0` (all destinations).
- `dport` - (Optional, Forces new resource) Destination port or port range (e.g. `80`, `8000:9000`). Only applicable to `tcp` and `udp` protocols.
- `enabled` - (Optional) Whether the rule is currently active. Defaults to `true`. Mutable without forcing a new resource.
- `position` - (Optional) Evaluation order — lower values are evaluated first. Defaults to `100`. Mutable without forcing a new resource.
- `comment` - (Optional) Human-readable description of the rule's purpose. Mutable without forcing a new resource.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the firewall rule.
- `created_at` - Timestamp of creation (RFC3339).

## Import

Firewall rules can be imported using the VNet ID and rule ID separated by a slash:

```
terraform import ccp_vnet_firewall_rule.allow_http <vnet_id>/<rule_id>
```
