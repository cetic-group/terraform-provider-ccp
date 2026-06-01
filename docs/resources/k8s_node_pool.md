---
page_title: "ccp_k8s_node_pool Resource - ccp"
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
  cluster_id = ccp_k8s_cluster.prod.id
  name       = "general-workers"
  plan       = "large"
  replicas   = 3
  min_size   = 2
  max_size   = 10
  labels = {
    "workload-type" = "general"
    "env"           = "prod"
  }
}

# Memory-optimised pool for data workloads — dedicated with taint
resource "ccp_k8s_node_pool" "data_workers" {
  cluster_id = ccp_k8s_cluster.prod.id
  name       = "data-workers"
  plan       = "xlarge"
  replicas   = 2
  labels = {
    "workload-type" = "data"
  }
  taints = [
    {
      key    = "workload-type"
      value  = "data"
      effect = "NoSchedule"
    }
  ]
}
```

## Argument Reference

### Required

- `cluster_id` - (Required, Forces new resource) UUID of the parent Kubernetes cluster.
- `name` - (Required, Forces new resource) Name of the node pool. Must be unique within the cluster.
- `plan` - (Required, Forces new resource) Instance plan for worker nodes. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `replicas` - (Required) Desired number of worker nodes in this pool.

### Optional

- `min_size` - (Optional) Minimum number of nodes when autoscaling is enabled. Must be greater than or equal to 1.
- `max_size` - (Optional) Maximum number of nodes when autoscaling is enabled. Must be greater than or equal to `replicas`.
- `labels` - (Optional) Map of Kubernetes node labels to apply to all nodes in this pool (e.g. `{ "workload-type" = "gpu" }`). Labels in the `kubernetes.io/*` namespace are propagated via the MachineDeployment metadata.
- `taints` - (Optional) Set of Kubernetes taints to apply to all nodes in this pool. Each taint has the following attributes:
  - `key` - (Required) Taint key.
  - `value` - (Optional) Taint value (may be empty).
  - `effect` - (Required) Taint effect. One of: `NoSchedule`, `PreferNoSchedule`, `NoExecute`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the node pool.
- `status` - Current status. Possible values: `provisioning`, `active`, `scaling`, `error`.

## Import

Node pools can be imported using `<cluster_id>/<pool_id>`:

```
terraform import ccp_k8s_node_pool.workers <cluster_id>/<pool_id>
```
