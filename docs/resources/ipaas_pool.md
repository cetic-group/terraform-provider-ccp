---
page_title: "ccp_ipaas_pool Resource - cetic-cloud-platform"
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
  name   = "orange-rnn-01"
  region = "RNN"
  cidr   = "198.51.100.0/28"
  kind   = "ipaas_routed"
}

# Legacy OPNsense pool (existing deployments only)
resource "ccp_ipaas_pool" "legacy_rnn" {
  name   = "opnsense-legacy"
  region = "RNN"
  cidr   = "203.0.113.0/29"
  kind   = "legacy"
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name for the pool.
- `region` - (Required, Forces new resource) Region where the pool is allocated. One of: `RNN`, `PAR`, `ABJ`.
- `cidr` - (Required, Forces new resource) CIDR block of the IP pool (e.g. `198.51.100.0/28`). Must be a valid IPv4 CIDR with prefix length between `/24` and `/29`.
- `kind` - (Required, Forces new resource) Pool type. One of: `ipaas_routed` (BGP-announced via edge node), `legacy` (OPNsense DNAT).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the IP pool.

## Import

IP pools can be imported using their UUID:

```
terraform import ccp_ipaas_pool.orange_rnn_01 <pool_id>
```
