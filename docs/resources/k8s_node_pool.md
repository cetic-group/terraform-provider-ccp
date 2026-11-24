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

# Worker pool with an explicit Kubernetes version. Upgrading the control plane
# (`ccp_k8s_cluster.k8s_version`) no longer moves the pools: raise this value
# after it — in the same apply is fine — to upgrade the pool, one node at a time.
resource "ccp_k8s_node_pool" "staged_workers" {
  cluster_id  = ccp_k8s_cluster.prod.id
  name        = "staged-workers"
  plan        = "medium"
  replicas    = 2
  k8s_version = "v1.34.8" # <= the control plane, same minor to upgrade
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

- `k8s_version` - (Optional, Computed) Desired Kubernetes version of the worker nodes in this pool, in `vX.Y.Z` format (e.g. `v1.34.8`). Omit to keep the version the platform gives the pool (the control-plane version at creation; since cetic-cloud-platform#2143 the platform pins a pool on its version when the control plane is upgraded — read back without a diff). Raising it upgrades the pool **in place** through the platform's pool upgrade: the nodes are replaced one at a time (drains respect the PodDisruptionBudgets) and the provider waits for the end. The target must be `<=` the control-plane version and of the same minor; if another upgrade of the cluster is running, the provider waits for it first. The platform may move the pool **up** on its own (automatic patch, console): while the pool runs the configured version or a higher one, the value stays as written and no diff is shown. A configured value below the running version is recorded with a warning; the provider never downgrades a pool. Format `X.Y.Z` with an optional `v`, checked at validation. A **failed upgrade** is an error of the apply with the platform's message (pool `status = "error"`); the next apply relaunches it with the same version (cetic-cloud-platform#2146; an older platform answers with an error saying to relaunch it from the console). Several pools of one apply are upgraded one after the other: a pool waits for the upgrades of the others (they are not counted as failures).
- `disk_gb` - (Optional, Computed, Forces new resource) Root disk size in GB of every node in the pool. When omitted the platform picks 20 GB (or the plan's minimum disk if higher) and the value is read back without a diff — changing another attribute (e.g. `replicas`) never replaces the pool. No resize endpoint exists for node pools, so changing this value forces replacement of the pool.
- `min_size` - (Optional) Minimum number of nodes when autoscaling is enabled. Must be greater than or equal to 1.
- `max_size` - (Optional) Maximum number of nodes when autoscaling is enabled. Must be greater than or equal to `replicas`.
- `labels` - (Optional) Map of Kubernetes node labels to apply to all nodes in this pool (e.g. `{ "workload-type" = "gpu" }`). Labels in the `kubernetes.io/*` namespace are propagated via the MachineDeployment metadata.
- `taints` - (Optional) Set of Kubernetes taints to apply to all nodes in this pool. Each taint has the following attributes:
  - `key` - (Required) Taint key.
  - `value` - (Optional) Taint value (may be empty).
  - `effect` - (Required) Taint effect. One of: `NoSchedule`, `PreferNoSchedule`, `NoExecute`.

### Optional — waiting times (`timeouts`)

- `update` - Each wait of a version upgrade: the other upgrades of the cluster before starting (default `30m`), then the upgrade of the pool (default `90m`). Set, the value applies to both. Go duration (`30m`, `1h30m`), never sent to the platform.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the node pool.
- `current_k8s_version` - Kubernetes version the pool's nodes actually run: the platform's `k8s_version_effective`, updated at the end of an upgrade; on a platform that does not report it yet, the version last requested (the target during or after a failed upgrade — check `status`). `null` only on a platform that does not report `k8s_version_effective`, for a pool created without `k8s_version` and never pinned since (it then runs the control-plane version). While the pool is not `active` (being created or upgraded), the reported versions are provisional: `k8s_version` keeps its known value until the pool is `active`.
- `status` - Current status. Possible values: `provisioning`, `active`, `updating`, `error`, `deleting`.

## Import

Node pools can be imported using `<cluster_id>/<pool_id>`:

```
terraform import ccp_k8s_node_pool.workers <cluster_id>/<pool_id>
```
