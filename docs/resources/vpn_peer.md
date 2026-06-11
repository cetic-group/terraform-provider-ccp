---
page_title: "ccp_vpn_peer Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a WireGuard VPN peer (client) of a ccp_vpn_gateway.
---

# ccp_vpn_peer (Resource)

Manages a **VPN peer** — a registered WireGuard client of a [`ccp_vpn_gateway`](vpn_gateway.md). Two enrollment models are supported, selected by whether you supply `public_key`:

- **Model A — bring-your-own-key.** You generate a WireGuard keypair yourself and pass the public key via `public_key`. The platform never sees a private key, so the returned `config` is a skeleton with no `PrivateKey` line. `store_private_key` / `one_time` have no effect.
- **Model B — server-generated.** You omit `public_key`. The platform generates a keypair and (when `store_private_key` is `true`, the default) returns a ready-to-use `config` containing the **private key**. Set `one_time = true` to make the config retrievable only once.

~> **Note:** `gateway_id` and `name` are immutable after creation. Changing either, or any Model-B knob, forces replacement. The CETIC Cloud API has no peer-update endpoint.

~> **Note (sensitive):** In Model B the `config` attribute embeds the peer's private key and is marked **sensitive**. It is returned **only at create time**; the API never re-exposes it on read, so the provider preserves it in state. After `terraform import` the `config` cannot be recovered.

## Example Usage

### Model B — let the platform generate the keypair

```hcl
resource "ccp_vpn_peer" "laptop" {
  gateway_id = ccp_vpn_gateway.ops.id
  name       = "alice-laptop"
  # public_key omitted => server generates the keypair (Model B)
  store_private_key = true
  one_time          = false
}

# The full client config (contains the private key) — handle as a secret.
output "laptop_wireguard_config" {
  value     = ccp_vpn_peer.laptop.config
  sensitive = true
}
```

### Model A — bring your own key

```hcl
resource "ccp_vpn_peer" "router" {
  gateway_id = ccp_vpn_gateway.ops.id
  name       = "branch-router"
  public_key = "abc123...=" # your own WireGuard public key
}
```

## Argument Reference

### Required

- `gateway_id` - (Required, Forces new resource) UUID of the `ccp_vpn_gateway` this peer connects to.
- `name` - (Required, Forces new resource) Human-readable name for the peer (max 100 chars; letters, digits, `_`, `-`, and spaces).

### Optional

- `public_key` - (Optional, Computed, Forces new resource) WireGuard public key of the client. Set it for **Model A** (bring-your-own-key); omit it for **Model B** (server-generated), in which case it is populated from the response.
- `store_private_key` - (Optional, Computed, Forces new resource) **Model B only.** When `true` (default) the server-generated private key is embedded in `config`. Ignored when `public_key` is set.
- `one_time` - (Optional, Computed, Forces new resource) **Model B only.** When `true` the generated `config` is retrievable only once (at create). Defaults to `false`. Ignored when `public_key` is set.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the peer.
- `ip` - Tunnel IP assigned to the peer from the gateway's peer pool.
- `model` - Enrollment model resolved by the server: `byok` (Model A) or `generated` (Model B).
- `config` - **Sensitive.** Full WireGuard client configuration. In Model B it contains the peer's private key. Returned only at create time and preserved in state thereafter.

## Import

VPN peers can be imported using `<gateway_id>/<peer_id>`:

```
terraform import ccp_vpn_peer.laptop <gateway_id>/<peer_id>
```

~> The create-only `config` (and, in Model B, the embedded private key) cannot be recovered on import.
