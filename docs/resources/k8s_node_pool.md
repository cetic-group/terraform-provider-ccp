---
page_title: "ccp_k8s_node_pool Resource - cetic-cloud-platform"
subcategory: "Kubernetes"
description: |-
  Manages a node pool within a Kubernetes cluster on CETIC Cloud Platform.
---

# ccp_k8s_node_pool (Resource)

Manages a node pool (MachineDeployment) within a CETIC Cloud Kubernetes cluster. Each pool defines the instance plan and desired replica count for a group of worker nodes. Multiple pools with different plans allow you to separate workloads (e.g. general-purpose workers vs GPU-intensive jobs).

~> **Note:** Node pool creation is asynchronous. The provider polls until the pool reaches `active` status. Changing `replicas` scales the pool in place. Changing `cluster_id` or `name` forces a new resource.

## Example Usage

```hcl
# General-purpose worker pool with autoscaling
resource "ccp_k8s_node_pool" "workers" {
  cluster_id         = ccp_k8s_cluster.prod.id
  name               = "general-workers"
  plan               = "large"
  replicas           = 3
  autoscaler_enabled = true
  min_size           = 2
  max_size           = 10
  labels = {
    "workload-type" = "general"
    "env"           = "prod"
  }
}

# Memory-optimised pool for data workloads
resource "ccp_k8s_node_pool" "data_workers" {
  cluster_id = ccp_k8s_cluster.prod.id
  name       = "data-workers"
  plan       = "xlarge"
  replicas   = 2
  labels = {
    "workload-type" = "data"
  }
}
```

## Argument Reference

### Required

- `cluster_id` - (Required, Forces new resource) UUID of the parent Kubernetes cluster.
- `name` - (Required, Forces new resource) Name of the node pool. Must be unique within the cluster.
- `plan` - (Required) Instance plan for worker nodes. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `replicas` - (Required) Desired number of worker nodes in this pool.

### Optional

- `autoscaler_enabled` - (Optional) Enable autoscaling for this node pool. Requires the parent cluster to have `autoscaler_enabled = true`. When enabled, `min_size` and `max_size` define the scaling bounds. Defaults to `false`.
- `min_size` - (Optional) Minimum number of nodes when autoscaling is enabled. Must be greater than or equal to 1. Defaults to `1`.
- `max_size` - (Optional) Maximum number of nodes when autoscaling is enabled. Must be greater than or equal to `replicas`. Defaults to `10`.
- `labels` - (Optional) Map of Kubernetes node labels to apply to all nodes in this pool (e.g. `{ "workload-type" = "gpu" }`). Labels in the `kubernetes.io/*` namespace are propagated via the MachineDeployment metadata, not via kubelet flags.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the node pool.
- `status` - Current status. Possible values: `provisioning`, `active`, `scaling`, `error`.
- `current_replicas` - Current number of ready worker nodes in this pool.

## Import

Node pools can be imported using their UUID:

```
terraform import ccp_k8s_node_pool.workers <node_pool_id>
```
