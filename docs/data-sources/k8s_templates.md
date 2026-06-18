---
page_title: "ccp_k8s_templates Data Source - ccp"
subcategory: "Catalogs"
description: |-
  Lists Kubernetes node OS templates available on CETIC Cloud.
---

# ccp_k8s_templates (Data Source)

Lists Kubernetes node OS templates from the CETIC Cloud catalog (admin-managed). Use the `os_key` field to populate `ccp_k8s_node_pool.os_key`.

## Example Usage

```hcl
data "ccp_k8s_templates" "available" {}

# Pick the latest Flatcar image for the RNN region
locals {
  rnn_flatcar = one(
    [for t in data.ccp_k8s_templates.available.templates :
      t if t.region == "RNN" && t.os_label == "Flatcar"
    ],
  )
}

resource "ccp_k8s_node_pool" "workers" {
  cluster_id    = ccp_k8s_cluster.app.id
  name          = "workers"
  region        = "RNN"
  plan          = "medium"
  desired_count = 3
  os_key        = local.rnn_flatcar.os_key
}
```

## Attributes Reference

- `templates` - List of available K8s node OS templates.
  - `os_key` - OS template key (used in `ccp_k8s_node_pool.os_key`). Example: `ccks-capi-flatcar-k1346`.
  - `os_label` - Human-readable OS label (e.g. `Flatcar`).
  - `os` - Node OS family slug for the template. One of `flatcar`, `ubuntu`, `rocky9`.
  - `display_name` - Display name shown in the console.
  - `k8s_version` - Kubernetes version baked into the template.
  - `region` - Region in which the template lives.
  - `vmid` - Internal template ID (admin-only field, may be null).
  - `built_at` - Timestamp at which the template was last built.
