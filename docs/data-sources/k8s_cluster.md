---
page_title: "ccp_k8s_cluster Data Source - ccp"
subcategory: "Kubernetes"
description: |-
  Look up an existing CETIC Cloud Kubernetes (CCKS) cluster by ID or by (name, region).
---

# ccp_k8s_cluster (Data Source)

Look up an existing CETIC Cloud Kubernetes cluster (CCKS) by `id`, or by the unique combination `(name, region)`. Useful to reference a cluster that was created outside of the current Terraform configuration (manually via the console, by a sibling stack, or by another team).

## Example Usage

```hcl
# By ID
data "ccp_k8s_cluster" "prod" {
  id = "11111111-2222-3333-4444-555555555555"
}

# By (name, region)
data "ccp_k8s_cluster" "prod_alt" {
  name   = "prod-cluster"
  region = "RNN"
}

output "k8s_endpoint" {
  value = data.ccp_k8s_cluster.prod.api_endpoint
}

output "k8s_proxy_vip" {
  # Only populated when tier = "prod".
  value = data.ccp_k8s_cluster.prod.proxy_vip_vnet
}
```

## Argument Reference

Provide **either** `id`, **or** the pair `(name, region)`. Combining the two yields an error. Providing neither yields an error.

### Optional

- `id` — UUID of the cluster to look up.
- `name` — Name of the cluster. Combine with `region`.
- `region` — Region of the cluster. Combine with `name`.

## Attributes Reference

- `id` — UUID of the cluster.
- `name` — DNS-safe slug of the cluster.
- `display_name` — Human-readable name (nullable).
- `region` — Region code (`RNN`, `PAR`, `ABJ`, …).
- `k8s_version` — Kubernetes version currently deployed (e.g. `v1.31.0`).
- `os_template_key` — QEMU template key used to provision worker nodes.
- `vpc_id` — UUID of the VPC the cluster lives in.
- `vnet_id` — UUID of the VNet the cluster nodes are attached to.
- `pod_cidr` — Pod network CIDR.
- `service_cidr` — Service network CIDR.
- `api_endpoint` — Kubernetes API endpoint URL (`https://host:port`), available once the cluster is `active`.
- `apiserver_public_ip_id` — UUID of the public IP attached to the apiserver (nullable).
- `public_ip_address` — Public IP address of the apiserver (nullable).
- `autoscaler_scale_down_delay_after_add` — Cluster Autoscaler — duration to wait before scaling down after a scale-up event.
- `autoscaler_scale_down_unneeded_time` — Cluster Autoscaler — duration a node must be unneeded before removal.
- `ingress_controller_enabled` — Whether an ingress controller is deployed in the cluster.
- `ingress_controller_scope` — `internal` (VNet only) or `external` (public IP).
- `ingress_controller_class` — `incluster` (Cilium IPAM) or `managed` (CETIC Cloud LXC LB).
- `ingress_public_ip_id` — UUID of the public IP pre-reserved for the ingress controller (nullable).
- `ingress_public_ip_address` — Effective public IP address of the ingress (nullable).
- `ingress_internal_ip` — VNet IP pre-reserved for the ingress controller (nullable).
- `tier` — Topology of the LXC proxy fronting the apiserver:
    * `dev` — single LXC proxy (SPOF acceptable in dev/staging).
    * `prod` — 2 LXC proxies (primary + secondary) with Keepalived VRRP and a floating VIP (HA).
- `proxy_secondary_vmid` — Proxmox VMID of the secondary LXC proxy. Null when `tier = "dev"`.
- `proxy_secondary_node` — Proxmox node hosting the secondary LXC proxy. Null when `tier = "dev"`.
- `proxy_vip_vnet` — Keepalived VRRP floating VIP shared between the LXC proxies. Null when `tier = "dev"`.
- `status` — `creating`, `provisioning`, `active`, `updating`, `error`, or `deleting`.
- `error_message` — Last error message reported by the provisioning workflow (nullable).
- `tags` — Free-form tags.
- `created_at` — RFC 3339 creation timestamp.
- `updated_at` — RFC 3339 last-update timestamp.

~> **Note:** The data source does not expose `kubeconfig` — fetch it via [`ccp_k8s_cluster`](../resources/k8s_cluster.md) (resource) or via the CETIC Cloud console / `cetic` CLI.
