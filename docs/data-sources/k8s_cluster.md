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
  # Only populated on a paid plan (standard, pro).
  value = data.ccp_k8s_cluster.prod.proxy_vip_vnet
}

output "k8s_patch" {
  # null when the cluster runs the newest patch of its minor.
  value = try(data.ccp_k8s_cluster.prod.patch_disponible.version, null)
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
- `k8s_version` — Kubernetes version requested for the control plane (e.g. `1.34.8`): the target from the start of an upgrade, also when it failed (the cluster then stays `active` with `error_message`; `error` on a platform older than cetic-cloud-platform#2146).
- `os_image` — Node operating-system family (`flatcar`, `ubuntu`, `rocky10`, `rocky9`).
- `os_template_key` — VM template key used to provision worker nodes.
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
- `ingress_controller_class` — `incluster` (ingress deployed inside the cluster, no extra load balancer) or `managed` (a dedicated CETIC Cloud load balancer in front of the cluster).
- `ingress_public_ip_id` — UUID of the public IP pre-reserved for the ingress controller (nullable).
- `ingress_public_ip_address` — Effective public IP address of the ingress (nullable).
- `ingress_internal_ip` — VNet IP pre-reserved for the ingress controller (nullable).
- `plan` — Plan of the control plane: `starter` (single frontend, no HA), `standard` or `pro` (redundant frontend with automatic failover on a floating address).
- `tier` — **Deprecated**, use `plan`. `dev` for `starter`, `prod` for a paid plan.
- `etcd_volume_gb` — Effective etcd volume (GiB).
- `node_ip_block_size` — Size of the node IP block allocated in the VNet (nullable).
- `proxy_vip_vnet` — Floating address on the VNet shared between the two apiserver frontends — the address the kubeconfig points at when HA is active. Null on `starter`.
- `patch_disponible` — Newest Kubernetes patch of the same minor available for the cluster, `null` when none: `version`, `os`, `templates_ok` (a template exists for the OS of every pool concerned), `plan_de_controle` (the control plane is concerned), `pools` (names of the pools concerned), `duree_estimee_minutes`.
- `upgrade_en_cours` — Upgrade running or waiting for its maintenance window, `null` when none: `id`, `type` (`plan_de_controle`, `pool`, `patch`, `mineure`), `origine` (`manuelle`, `automatique`), `statut` (`en_cours`, `en_pause`), `version_depart`, `version_cible`.
- `oidc_issuer_url` — Issuer of the SSO OIDC configured on the apiserver, `null` without SSO.
- `status` — `creating`, `provisioning`, `active`, `updating`, `error`, or `deleting`.
- `error_message` — Last error message reported by the provisioning workflow (nullable).
- `tags` — Free-form tags.
- `created_at` — RFC 3339 creation timestamp.
- `updated_at` — RFC 3339 last-update timestamp.

~> **Note:** The data source does not expose `kubeconfig` — fetch it via [`ccp_k8s_cluster`](../resources/k8s_cluster.md) (resource) or via the CETIC Cloud console / `cetic` CLI.
