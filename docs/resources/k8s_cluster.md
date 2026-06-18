---
page_title: "ccp_k8s_cluster Resource - ccp"
subcategory: "Kubernetes"
description: |-
  Manages a Kubernetes cluster on CETIC Cloud Platform.
---

# ccp_k8s_cluster (Resource)

Manages a Kubernetes cluster on CETIC Cloud Platform. The cluster runs inside the tenant VPC with Cilium CNI, persistent block storage, and an in-cluster operator that automates `LoadBalancer` service IP allocation from the tenant's public IP pool.

~> **Note:** Cluster provisioning is fully asynchronous and involves bootstrapping the control plane, joining workers, and deploying cluster add-ons (Cilium, CSI, cluster-agent). The provider polls until the cluster reaches `active` status, which typically takes 8 to 15 minutes.

~> **Note:** `k8s_version` is mutable and triggers an in-place rolling upgrade when changed. Upgrades apply to both control-plane nodes and all node pools. Expect ~20 minutes of rolling upgrade time. The cluster remains available during the upgrade.

→ **End-to-end Terraform examples** for every ingress controller and apiserver exposure pattern: see the [public documentation page](https://docs.cloud.cetic-group.com/terraform/ccp_k8s_cluster).

## Example Usage — minimal

```hcl
resource "ccp_k8s_cluster" "dev" {
  name            = "dev-cluster"
  region          = "RNN"
  vpc_id          = ccp_vpc.main.id
  vnet_id         = ccp_vnet.workers.id
  k8s_version     = "v1.34.8"
  os_template_key = "kube-v1-34-8"
}
```

## Example Usage — choose the node OS family

```hcl
resource "ccp_k8s_cluster" "ubuntu" {
  name            = "ubuntu-cluster"
  region          = "RNN"
  vpc_id          = ccp_vpc.main.id
  vnet_id         = ccp_vnet.workers.id
  k8s_version     = "v1.34.8"
  os_template_key = "kube-v1-34-8"
  os_image        = "ubuntu" # node OS family — one of flatcar (default), ubuntu, rocky9
}
```

Default behavior:
- `tier = "dev"` → single apiserver frontend (SPOF acceptable in dev).
- `os_image = "flatcar"` → nodes run Flatcar Container Linux unless overridden.
- `ingress_controller_enabled = true`, `ingress_controller_class = "incluster"`, `ingress_controller_scope = "internal"` → ingress handled by Cilium inside the cluster on an auto-allocated VNet IP.
- Apiserver private only (no public IP).
- Pod CIDR `10.244.0.0/16`, service CIDR `10.96.0.0/12`.

## Example Usage — production HA with public ingress

```hcl
resource "ccp_public_ip" "apiserver" {
  region = "RNN"
}

resource "ccp_public_ip" "ingress" {
  region = "RNN"
}

resource "ccp_k8s_cluster" "prod" {
  name            = "prod-cluster"
  region          = "RNN"
  vpc_id          = ccp_vpc.main.id
  vnet_id         = ccp_vnet.workers.id
  k8s_version     = "v1.34.8"
  os_template_key = "kube-v1-34-8"
  tier            = "prod" # HA: redundant apiserver frontend with automatic failover

  # Apiserver: public access
  apiserver_public_ip_id = ccp_public_ip.apiserver.id

  # Ingress: public, handled in-cluster (no extra load balancer)
  ingress_controller_enabled = true
  ingress_controller_class   = "incluster"
  ingress_controller_scope   = "external"
  ingress_public_ip_id       = ccp_public_ip.ingress.id

  autoscaler_scale_down_delay_after_add = "10m"
  autoscaler_scale_down_unneeded_time   = "10m"

  # Initial worker pool pinned one minor behind the control plane.
  # Omit k8s_version to inherit the control-plane version.
  initial_pool {
    name        = "default"
    plan        = "small"
    replicas    = 2
    k8s_version = "v1.33.4" # must be <= the control-plane k8s_version
  }

  tags = ["k8s", "env:prod"]
}
```

## Argument Reference

### Required

- `name` - Name of the Kubernetes cluster.
- `region` - (Forces new resource) Region where the cluster is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Forces new resource) UUID of the VPC.
- `vnet_id` - (Forces new resource) UUID of the VNet where worker nodes attach.
- `k8s_version` - Kubernetes version (e.g. `"v1.34.8"`). Mutable — triggers a rolling upgrade when changed. Must match an `os_template_key` available in the region — list available versions via the [`ccp_k8s_templates`](../data-sources/k8s_templates.md) data source.
- `os_template_key` - (Forces new resource) OS template key for worker nodes (e.g. `"kube-v1-34-8"`). Must match the chosen `k8s_version` and be available in the region.

### Optional — cluster topology

- `display_name` - Display name shown in the console. Defaults to `name`.
- `os_image` - (Forces new resource) Operating-system family for the cluster nodes. One of `flatcar`, `ubuntu`, `rocky9`. Defaults to `flatcar` when omitted. Changing the node OS forces destroy + recreate.
- `tier` - (Forces new resource) Topology of the apiserver frontend:
    * `dev` (default) — single frontend (SPOF acceptable in dev/staging).
    * `prod` — redundant frontend (primary + secondary) with automatic failover on a floating address, providing HA at the apiserver layer.

    Immutable on the backend — changing `tier` forces destroy + recreate.
- `pod_cidr` - (Forces new resource) Pod IP range. Default `"10.244.0.0/16"`.
- `service_cidr` - (Forces new resource) Service IP range. Default `"10.96.0.0/12"`.
- `tags` - List of free-form tags (max 60, max 50 chars each).
- `initial_pool` - Block describing the worker pool created with the cluster. Attributes:
    * `name` - (Forces new resource) Pool name. Default `"default"`.
    * `plan` - (Forces new resource) Instance plan (`nano` … `xlarge`). Default `"small"`.
    * `replicas` - Worker count. Mutable in-place (rolling).
    * `k8s_version` - (Optional) Kubernetes version of the worker nodes in the initial pool, in `vX.Y.Z` format (e.g. `v1.33.4`). Must be `<=` the cluster control-plane version (`k8s_version`); omit to inherit it. Mutable in-place (changing it triggers a rolling upgrade of the pool's nodes).
    * `labels` - Map of Kubernetes labels applied to the pool's nodes (parity with `ccp_k8s_node_pool.labels`). Mutable in-place.
    * `taints` - Set of Kubernetes taints (`{ key, value?, effect }`, `effect` ∈ `NoSchedule`/`PreferNoSchedule`/`NoExecute`) applied to the pool's nodes (parity with `ccp_k8s_node_pool.taints`). Mutable in-place.
    * `min_size` / `max_size` - Cluster autoscaler bounds (see *Optional — autoscaler*).

### Optional — apiserver exposure

The apiserver always has a private endpoint (auto-allocated from the VNet CIDR or pinned via `apiserver_internal_ip`). It can additionally be exposed publicly via `apiserver_public_ip_id`.

- `apiserver_internal_ip` - (Forces new resource) Pinned private IP for the apiserver. Defaults to auto-allocated within the VNet.
- `apiserver_public_ip_id` - UUID of a public IP attached to the apiserver (public kubeconfig). **Mutable** — set the UUID to attach, remove it (`null`) to detach, change it to rotate, all **without recreating the cluster**. Works both at create time and later. The IP must be in the same region as the cluster and come from a routed BYOIP pool; the cluster VNet must have SNAT enabled.

> **Migration (provider v3.0.0)**: the former separate `public_ip_id` attribute is removed — `apiserver_public_ip_id` is now the single, mutable knob for the apiserver public IP. If you used `public_ip_id`, rename it to `apiserver_public_ip_id`.

### Optional — ingress controller

CETIC Cloud Kubernetes Service (CCKS) ships with an ingress controller deployed in-cluster at bootstrap. The combination of `ingress_controller_class` × `ingress_controller_scope` determines the data path and the type of IP to pre-reserve.

- `ingress_controller_enabled` - Deploy an ingress controller at bootstrap. Mutable. Default `true`.
- `ingress_controller_class` - Implementation:
    * `incluster` (default) — Cilium-based ingress deployed inside the cluster. The IP is advertised by the workers themselves on the VNet (internal scope) or routed through CETIC Cloud's regional network (external scope). No extra load balancer to provision.
    * `managed` — A dedicated CETIC Cloud load balancer (HA pair with automatic failover) is provisioned in front of the cluster. Useful when you need L4 features like proxy protocol or per-listener TLS termination decoupled from the cluster.
- `ingress_controller_scope` - Network reachability:
    * `internal` (default) — Private IP on the VNet. Reachable only from inside the VPC (or via VPC peering).
    * `external` — Public IP routed through the regional IPaaS edge.
- `ingress_public_ip_id` - UUID of a pre-reserved public IP to use for the ingress (only meaningful with `scope = "external"`). If empty, the API auto-allocates one from the regional pool.
- `ingress_internal_ip` - Pre-reserved private VNet IP for the ingress (only meaningful with `scope = "internal"`). If empty, the API auto-allocates one from the VNet CIDR. The IP must be inside the VNet's range and outside the DHCP range.

#### Ingress controller — the four combinations

| `class` | `scope` | Use case | IP source | Characteristics |
|---------|---------|----------|-----------|-----------------|
| `incluster` | `internal` | Workloads only reached from inside the VPC (intranet, B2B over VPC peering) | Auto-allocated from VNet CIDR, or pin via `ingress_internal_ip` | Handled by the Cilium ingress controller deployed inside the cluster — minimal cost, no extra load balancer |
| `incluster` | `external` | Public services with the lowest possible cost | Auto-allocated, or pin via `ingress_public_ip_id` (region must match cluster) | The public IP routes to the workers through CETIC Cloud's regional network — no external load balancer to provision |
| `managed` | `internal` | Internal services that need L4 features (proxy protocol, custom listeners) | Auto-allocated, or pin via `ingress_internal_ip` | Dedicated load balancer, HA pair with automatic failover on a floating address — TLS and L4 features decoupled from the cluster |
| `managed` | `external` | Public services that need an external load balancer | Auto-allocated, or pin via `ingress_public_ip_id` | Dedicated load balancer exposed on the internet — TLS termination and full L4 control at a tier separate from the cluster |

### Optional — autoscaler

- `autoscaler_scale_down_delay_after_add` - Duration the autoscaler waits after a scale-up before considering scale-down (e.g. `"10m"`). Default `"10m"`.
- `autoscaler_scale_down_unneeded_time` - Duration a node must be unneeded before removal (e.g. `"10m"`). Default `"10m"`.

The per-pool autoscaler is configured via `min_size` / `max_size`:
- on the **initial pool** through the `initial_pool` block (`min_size` + `max_size`, mutable in-place — set both to enable, adjust to retune);
- on **additional pools** through [`ccp_k8s_node_pool`](k8s_node_pool.md).

Leave both `min_size`/`max_size` unset for a fixed-size pool. Enabling, retuning **and disabling** are all in-place: removing both `min_size`/`max_size` disables the autoscaler (the provider sends `0`/`0` → pool pinned to `replicas`), since provider v3.1.1/v3.1.2.

## Attributes Reference

In addition to all arguments above, the following attributes are exported.

### Cluster

- `id` - UUID of the cluster.
- `status` - Current status. Possible values: `provisioning`, `active`, `upgrading`, `error`.
- `kubeconfig` - (Sensitive) Kubeconfig file content (YAML) for accessing the cluster with `kubectl`. Store securely.
- `endpoint` - Kubernetes API server endpoint URL (e.g. `https://10.0.1.5:6443`).

### Apiserver

- `public_ip_address` - Public IP address of the apiserver, if attached. Empty otherwise.

### Ingress

- `ingress_public_ip_address` - Effective public IP of the ingress controller (Computed). Populated once the IP is reserved + bound — `scope = "external"` only.

### Apiserver frontend HA (tier = "prod")

These attributes are populated when `tier = "prod"` and exist for observability of the redundant frontend behind the apiserver. They are `null` when `tier = "dev"`.

- `proxy_secondary_vmid` - Internal identifier of the secondary apiserver frontend. Read-only.
- `proxy_secondary_node` - Internal placement of the secondary apiserver frontend. Read-only.
- `proxy_vip_vnet` - Floating address on the VNet shared between the two apiserver frontends — this is the IP the kubeconfig points at when HA is active.

## Import

Kubernetes clusters can be imported using their UUID:

```
terraform import ccp_k8s_cluster.prod <cluster_id>
```

## See also

- [Public docs — `ccp_k8s_cluster` end-to-end examples](https://docs.cloud.cetic-group.com/terraform/ccp_k8s_cluster) — full HCL for the 4 ingress combinations, autoscaler patterns, node pool composition.
- [`ccp_k8s_node_pool`](k8s_node_pool.md) — additional worker pools.
- [`ccp_k8s_templates` data source](../data-sources/k8s_templates.md) — list available `os_template_key` + `k8s_version` pairs per region.
- [`managed/k8s-cluster` Terraform module](https://github.com/cetic-group/cetic-cloud-terraform-modules/tree/main/modules/managed/k8s-cluster) — composable wrapper with sensible defaults.
