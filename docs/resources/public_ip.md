---
page_title: "ccp_public_ip Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a public IP address on CETIC Cloud Platform.
---

# ccp_public_ip (Resource)

Manages a public IP address allocated from a regional pool. Public IPs can be attached to containers, VM instances, load balancers, Kubernetes clusters, or database instances. IPs are routed via BGP (IPaaS) for low-latency, direct routing without NAT overhead.

~> **Note:** IP attachment and detachment are asynchronous operations. The `status` attribute transitions through `attaching` / `detaching` before reaching `attached` / `allocated`. The provider polls until the transition completes (up to 4 minutes).

## Example Usage

```hcl
# Allocate a public IP and attach to a container
resource "ccp_public_ip" "web" {
  region           = "RNN"
  attached_to_id   = ccp_container_instance.web.id
  attached_to_type = "container"
}

# Allocate without immediate attachment
resource "ccp_public_ip" "spare" {
  region = "RNN"
}
```

## Argument Reference

### Required

- `region` - (Required, Forces new resource) Region where the IP is allocated. One of: `RNN`, `PAR`, `ABJ`.

### Optional

- `attached_to_id` - (Optional) UUID of the resource to attach this IP to. When set, `attached_to_type` must also be specified.
- `attached_to_type` - (Optional) Type of resource to attach to. One of: `container`, `vm`, `load_balancer`, `db_instance`, `k8s_cluster`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the public IP.
- `ip_address` - The allocated public IP address (e.g. `198.51.100.42`).
- `status` - Current status. Possible values: `available`, `allocated`, `attaching`, `attached`, `detaching`, `error`.

## Import

Public IPs can be imported using their UUID:

```
terraform import ccp_public_ip.web <public_ip_id>
```
