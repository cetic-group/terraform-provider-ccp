---
page_title: "ccp_k8s_node_pool Data Source - ccp"
subcategory: "Kubernetes"
description: |-
  Look up a Kubernetes node pool by (id, cluster_id) or (name, cluster_id).
---

# ccp_k8s_node_pool (Data Source)

Look up a node pool within a CETIC Cloud Kubernetes cluster. `cluster_id` is always required.

## Example Usage

```hcl
data "ccp_k8s_cluster" "main" {
  name   = "prod"
  region = "RNN"
}

data "ccp_k8s_node_pool" "workers" {
  cluster_id = data.ccp_k8s_cluster.main.id
  name       = "workers"
}
```

## Argument Reference

### Required

- `cluster_id` — UUID of the parent K8s cluster.

### Optional (exactly one)

- `id` — UUID of the pool.
- `name` — Name of the pool within the cluster.

## Attributes Reference

- `id`, `cluster_id`, `name`, `plan`, `replicas`
- `labels` — Map of Kubernetes node labels.
- `taints` — List of `{key, value, effect}` taint objects.
- `min_size`, `max_size` (nullable, set when autoscaling is enabled)
- `machine_deployment_name` (nullable)
- `status`, `error_message` (nullable)
- `created_at`, `updated_at`
