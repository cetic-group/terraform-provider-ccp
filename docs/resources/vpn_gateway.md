---
page_title: "ccp_vpn_gateway Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a WireGuard VPN gateway on CETIC Cloud Platform.
---

# ccp_vpn_gateway (Resource)

Manages a **VPN gateway** — a managed WireGuard appliance that fronts the private networks of one or more VPCs. Remote clients (modelled by [`ccp_vpn_peer`](vpn_peer.md)) reach otherwise-unreachable private hosts (containers, VMs, …) through an encrypted tunnel instead of exposing instances to the public internet. The gateway exposes one public WireGuard endpoint (`endpoint_host:endpoint_port`) and a `public_key`; each peer's configuration references them.

~> **Note:** Every settable attribute (`name`, `region`, `plan`, `vpc_ids`, `public_ip_id`, `peer_pool_cidr`, `dns`, `tags`) is immutable after creation. To change any of them, create a new `ccp_vpn_gateway` and delete the old one. The CETIC Cloud API has no update endpoint for gateway core fields.

~> **Note:** Provisioning is asynchronous. Right after `terraform apply`, `status` is `provisioning` and `endpoint_host` / `endpoint_port` / `public_key` / `public_ip_address` may be empty; they are populated once the appliance becomes `active`. A subsequent `terraform refresh` reflects the final endpoint.

## Example Usage

```hcl
resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

# Optional: bring your own reserved public IP (otherwise IPaaS allocates one).
resource "ccp_public_ip" "vpn" {
  region = "RNN"
}

resource "ccp_vpn_gateway" "ops" {
  name   = "ops-vpn"
  region = "RNN"
  plan   = "small" # small | medium | large

  vpc_ids = [ccp_vpc.prod.id]

  public_ip_id   = ccp_public_ip.vpn.id
  peer_pool_cidr = "10.99.0.0/24"
  dns            = "10.0.0.2"

  tags = ["ops", "remote-access"]
}

output "vpn_endpoint" {
  value = "${ccp_vpn_gateway.ops.endpoint_host}:${ccp_vpn_gateway.ops.endpoint_port}"
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Human-readable name for the gateway (max 100 chars; letters, digits, `_`, `-`, and spaces).
- `region` - (Required, Forces new resource) Region code the gateway is provisioned in (e.g. `RNN`).
- `plan` - (Required, Forces new resource) Sizing plan: `small`, `medium`, or `large`.
- `vpc_ids` - (Required, Forces new resource) List of VPC UUIDs whose private networks the gateway tunnels into. The first entry is the primary VPC.

### Optional

- `public_ip_id` - (Optional, Computed, Forces new resource) UUID of a reserved public IP to attach to the gateway endpoint. If omitted, the platform allocates one (IPaaS).
- `peer_pool_cidr` - (Optional, Computed, Forces new resource) CIDR block the gateway allocates peer tunnel IPs from. If omitted, the platform picks one.
- `dns` - (Optional, Computed, Forces new resource) DNS server pushed to peers in their generated WireGuard config.
- `tags` - (Optional, Computed, Forces new resource) Free-form labels attached to the gateway.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the gateway.
- `status` - Lifecycle status: `provisioning`, `active`, `error`, or `deleting`. Read-only and volatile.
- `endpoint_host` - Public WireGuard endpoint hostname (or IP) clients connect to. Populated once the appliance finishes provisioning.
- `endpoint_port` - UDP port of the WireGuard endpoint. Populated once the appliance finishes provisioning.
- `public_key` - WireGuard public key of the gateway, needed in each peer's config. Populated once the appliance finishes provisioning.
- `public_ip_address` - Public IP address attached to the gateway endpoint. Populated once the appliance finishes provisioning.

## Import

VPN gateways can be imported using their UUID:

```
terraform import ccp_vpn_gateway.ops <gateway_id>
```
