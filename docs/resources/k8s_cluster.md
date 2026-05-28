---
page_title: "ccp_k8s_cluster Resource - cetic-cloud-platform"
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

Default behavior:
- `tier = "dev"` → 1 LXC proxy fronting the apiserver (SPOF acceptable in dev).
- `ingress_controller_enabled = true`, `ingress_controller_class = "incluster"`, `ingress_controller_scope = "internal"` → Cilium L2 announce on a private VNet IP, auto-allocated.
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
  tier            = "prod" # HA: 2 LXC proxies + Keepalived VRRP + floating VIP

  # Apiserver: public access
  apiserver_public_ip_id = ccp_public_ip.apiserver.id

  # Ingress: public, in-cluster Cilium L2 announce
  ingress_controller_enabled = true
  ingress_controller_class   = "incluster"
  ingress_controller_scope   = "external"
  ingress_public_ip_id       = ccp_public_ip.ingress.id

  autoscaler_scale_down_delay_after_add = "10m"
  autoscaler_scale_down_unneeded_time   = "10m"

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
- `tier` - (Forces new resource) Topology of the LXC proxy fronting the apiserver:
    * `dev` (default) — single LXC proxy (SPOF acceptable in dev/staging).
    * `prod` — 2 LXC proxies (primary + secondary) with Keepalived VRRP and a floating VIP, providing HA at the proxy layer.

    Immutable on the backend — changing `tier` forces destroy + recreate.
- `pod_cidr` - (Forces new resource) Pod IP range. Default `"10.244.0.0/16"`.
- `service_cidr` - (Forces new resource) Service IP range. Default `"10.96.0.0/12"`.
- `tags` - List of free-form tags (max 60, max 50 chars each).

### Optional — apiserver exposure

The apiserver always has a private endpoint (auto-allocated from the VNet CIDR or pinned via `apiserver_internal_ip`). It can additionally be exposed publicly via `apiserver_public_ip_id` (set at create time, immutable) **or** `public_ip_id` (mutable attach/detach).

- `apiserver_internal_ip` - (Forces new resource) Pinned private IP for the apiserver. Defaults to auto-allocated within the VNet.
- `apiserver_public_ip_id` - (Forces new resource) UUID of a public IP attached to the apiserver at create time. Use this for clusters that are public from day one. Once set, the IP is bound for the lifetime of the cluster.
- `public_ip_id` - UUID of a public IP attached to the apiserver. **Mutable** — supports attach/detach over the cluster lifetime without recreation. Use this when you want to add a public endpoint later, or rotate the public IP. Mutually exclusive with `apiserver_public_ip_id` at create time.

### Optional — ingress controller

CETIC Cloud Kubernetes Service (CCKS) ships with an ingress controller deployed in-cluster at bootstrap. The combination of `ingress_controller_class` × `ingress_controller_scope` determines the data path and the type of IP to pre-reserve.

- `ingress_controller_enabled` - Deploy an ingress controller at bootstrap. Mutable. Default `true`.
- `ingress_controller_class` - Implementation:
    * `incluster` (default) — Cilium L2 announce. The IP is advertised by the workers themselves on the VNet (ARP for internal scope, BGP via IPaaS edge for external). No extra infrastructure.
    * `managed` — A CETIC Cloud LB (LXC + HAProxy/Keepalived) is provisioned in front of the cluster. Useful when you need L4 features like proxy protocol or per-listener TLS termination decoupled from the cluster.
- `ingress_controller_scope` - Network reachability:
    * `internal` (default) — Private IP on the VNet. Reachable only from inside the VPC (or via VPC peering).
    * `external` — Public IP routed through the regional IPaaS edge.
- `ingress_public_ip_id` - UUID of a pre-reserved public IP to use for the ingress (only meaningful with `scope = "external"`). If empty, the API auto-allocates one from the regional pool.
- `ingress_internal_ip` - Pre-reserved private VNet IP for the ingress (only meaningful with `scope = "internal"`). If empty, the API auto-allocates one from the VNet CIDR. The IP must be inside the VNet's range and outside the DHCP range.

#### Ingress controller — the four combinations

| `class` | `scope` | Use case | IP source | Data path |
|---------|---------|----------|-----------|-----------|
| `incluster` | `internal` | Workloads only reached from inside the VPC (intranet, B2B over VPC peering) | Auto-allocated from VNet CIDR, or pin via `ingress_internal_ip` | Cilium L2 announce → ARP on VNet bridge → worker pod |
| `incluster` | `external` | Public services with the lowest possible cost (no extra LB) | Auto-allocated, or pin via `ingress_public_ip_id` (region must match cluster) | IPaaS edge (DNAT) → WG tunnel → NAT GW → BGP /32 → worker pod (Cilium kube-proxy replacement intercepts in BPF) |
| `managed` | `internal` | Internal services that need L4 features (proxy protocol, custom listeners) | Auto-allocated, or pin via `ingress_internal_ip` | LB LXC pair (VRRP VIP) → worker NodePort → Cilium → pod |
| `managed` | `external` | Public services that need an external LB (TLS termination at the LB, full L4 control) | Auto-allocated, or pin via `ingress_public_ip_id` | IPaaS edge → LB public IP → LB LXC pair → worker NodePort → pod |

### Optional — autoscaler

- `autoscaler_scale_down_delay_after_add` - Duration the autoscaler waits after a scale-up before considering scale-down (e.g. `"10m"`). Default `"10m"`.
- `autoscaler_scale_down_unneeded_time` - Duration a node must be unneeded before removal (e.g. `"10m"`). Default `"10m"`.

The per-pool autoscaler is configured on [`ccp_k8s_node_pool`](k8s_node_pool.md) via `min_size` / `max_size`.

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

### Proxy topology (tier = "prod")

- `proxy_secondary_vmid` - Proxmox VMID of the secondary LXC proxy. `null` when `tier = "dev"`.
- `proxy_secondary_node` - Proxmox node hosting the secondary proxy. `null` when `tier = "dev"`.
- `proxy_vip_vnet` - Keepalived VRRP floating VIP shared between the two proxies. `null` when `tier = "dev"`.

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
