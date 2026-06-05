---
page_title: "ccp_ipaas_pool Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a public IP pool on CETIC Cloud Platform.
---

# ccp_ipaas_pool (Resource)

Manages a public IP pool from which tenant public IPs are allocated. IPaaS (IP as a Service) pools backed by `ipaas_routed` kind use BGP-announced BYOIP blocks — the CIDR is announced from a Scaleway edge node via FRR/BGP, and traffic is tunnelled to tenant NAT Gateways over WireGuard. Legacy pools use OPNsense DNAT.

~> **Note:** This resource requires administrator privileges. Standard tenant API keys cannot create or manage IP pools. Use this resource only in CETIC Cloud operator configurations.

## Example Usage

```hcl
# BGP-routed IPaaS pool for the Rennes region
resource "ccp_ipaas_pool" "orange_rnn_01" {
  region  = "RNN"
  cidr    = "198.51.100.0/28"
  gateway = "198.51.100.1"
}
```

## Argument Reference

### Required

- `region` - (Required, Forces new resource) Region where the pool is allocated. One of: `RNN`, `PAR`, `ABJ`.
- `cidr` - (Required, Forces new resource) CIDR block of the IP pool (e.g. `198.51.100.0/28`). Must be a valid IPv4 CIDR with prefix length between `/24` and `/29`.
- `gateway` - (Required, Forces new resource) Gateway IP of the pool (first usable address, reserved by the upstream routing).

### Optional

- `edge_id` - (Optional) UUID of the edge node that announces this pool over BGP.
- `is_active` - (Optional, Computed) Whether the pool is available for allocation. Defaults to `true`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the IP pool.

## Import

IP pools can be imported using their UUID:

```
terraform import ccp_ipaas_pool.orange_rnn_01 <pool_id>
```
