---
page_title: "ccp_k8s_cluster Resource - cetic-cloud-platform"
subcategory: "Kubernetes"
description: |-
  Manages a Kubernetes cluster on CETIC Cloud Platform.
---

# ccp_k8s_cluster (Resource)

Manages a Kubernetes cluster on CETIC Cloud Platform. The cluster runs inside the tenant VPC with Cilium CNI, persistent block storage, and an in-cluster operator that automates `LoadBalancer` service IP allocation from the tenant's public IP pool.

~> **Note:** Cluster provisioning is fully asynchronous and involves bootstrapping control plane VMs, joining workers, and deploying cluster add-ons (Cilium, CSI, cluster-agent). The provider polls until the cluster reaches `active` status, which typically takes 8 to 15 minutes.

~> **Note:** `k8s_version` is mutable and triggers an in-place rolling upgrade when changed. Upgrades apply to both control-plane nodes and all node pools. Expect ~20 minutes of rolling upgrade time. The cluster remains available during the upgrade.

## Example Usage

```hcl
resource "ccp_public_ip" "apiserver" {
  region = "RNN"
}

resource "ccp_k8s_cluster" "prod" {
  name                         = "prod-cluster"
  region                       = "RNN"
  vpc_id                       = ccp_vpc.main.id
  vnet_id                      = ccp_vnet.web.id
  k8s_version                  = "1.31"
  autoscaler_enabled           = true
  scale_down_delay_after_add   = "10m"
  scale_down_unneeded_time     = "10m"
  ingress_controller_enabled   = true
  apiserver_public_ip_id       = ccp_public_ip.apiserver.id
  tags                         = ["k8s", "env:prod"]
}

resource "ccp_k8s_node_pool" "workers" {
  cluster_id         = ccp_k8s_cluster.prod.id
  name               = "general-workers"
  plan               = "large"
  replicas           = 3
  autoscaler_enabled = true
  min_size           = 2
  max_size           = 10
}

output "kubeconfig" {
  value     = ccp_k8s_cluster.prod.kubeconfig
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the Kubernetes cluster.
- `region` - (Required, Forces new resource) Region where the cluster is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC for the cluster.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where cluster nodes will be attached.
- `k8s_version` - (Required) Kubernetes minor version (e.g. `"1.31"`). Changing this value triggers an in-place rolling upgrade of the cluster.

### Optional

- `autoscaler_enabled` - (Optional) Enable the Cluster Autoscaler for this cluster. When enabled, node pools with `autoscaler_enabled = true` will scale automatically. Defaults to `false`.
- `scale_down_delay_after_add` - (Optional) Duration to wait before scaling down after a scale-up event (e.g. `"10m"`, `"30m"`). Only effective when `autoscaler_enabled = true`.
- `scale_down_unneeded_time` - (Optional) Duration a node must be unneeded before the autoscaler removes it (e.g. `"10m"`). Only effective when `autoscaler_enabled = true`.
- `ingress_controller_enabled` - (Optional) Deploy an ingress controller in the cluster at bootstrap. Defaults to `false`.
- `apiserver_public_ip_id` - (Optional) UUID of a public IP to expose the Kubernetes API server externally. The IP must be in the same region as the cluster.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the cluster.
- `status` - Current status. Possible values: `provisioning`, `active`, `upgrading`, `error`.
- `kubeconfig` - (Sensitive) Kubeconfig file content (YAML) for accessing the cluster with `kubectl`. Store securely.
- `endpoint` - Kubernetes API server endpoint URL (e.g. `https://10.0.1.5:6443`).
- `public_ip_address` - Public IP address of the API server if `apiserver_public_ip_id` is set, otherwise empty.

## Import

Kubernetes clusters can be imported using their UUID:

```
terraform import ccp_k8s_cluster.prod <cluster_id>
```
