---
page_title: "ccp_k8s_cluster Resource - ccp"
subcategory: "Kubernetes"
description: |-
  Manages a Kubernetes cluster on CETIC Cloud Platform.
---

# ccp_k8s_cluster (Resource)

Manages a Kubernetes cluster on CETIC Cloud Platform. The cluster runs inside the tenant VPC with Cilium CNI, persistent block storage, and an in-cluster operator that automates `LoadBalancer` service IP allocation from the tenant's public IP pool.

~> **Note:** Cluster provisioning is fully asynchronous and involves bootstrapping the control plane, joining workers, and deploying cluster add-ons (Cilium, CSI, cluster-agent). The provider polls until the cluster reaches `active` status, which typically takes 8 to 15 minutes.

~> **Note:** Raising `k8s_version` upgrades the **control plane only**, in place, one minor version at a time; the provider waits until the upgrade is over (the cluster goes `updating` → `active`). **Node pools no longer follow**: the platform pins every pool on the version it runs, and each pool is upgraded through its own `k8s_version` (`initial_pool.k8s_version`, [`ccp_k8s_node_pool.k8s_version`](k8s_node_pool.md)), after the control plane — in the same apply is fine. See [Kubernetes versions and upgrades](#kubernetes-versions-and-upgrades).

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
- `plan = "starter"` → free control plane, single apiserver frontend (no HA), up to 10 nodes.
- `os_image = "flatcar"` → nodes run Flatcar Container Linux unless overridden.
- `ingress_controller_enabled = true`, `ingress_controller_class = "incluster"`, `ingress_controller_scope = "internal"` → ingress handled by Cilium inside the cluster on an auto-allocated VNet IP.
- Apiserver private only (no public IP).
- Pod CIDR `10.244.0.0/16`, service CIDR `10.96.0.0/12`.

## Example Usage — isolated network (no internet access)

A cluster can run in an **isolated** subnet — one whose `snat` is `false`. The nodes no longer need
internet access to start: their images are preloaded and name resolution is served from inside the
network.

```hcl
resource "ccp_vnet" "airgap" {
  vpc_id = ccp_vpc.main.id
  name   = "airgap-workers"
  cidr   = "10.30.0.0/24"
  snat   = false # isolated: no outbound internet access
}

resource "ccp_k8s_cluster" "airgap" {
  name            = "airgap-cluster"
  region          = "RNN"
  vpc_id          = ccp_vpc.main.id
  vnet_id         = ccp_vnet.airgap.id
  k8s_version     = "v1.34.8"
  os_template_key = "kube-v1-34-8"

  # Keep the ingress internal: `external` needs a public address, which an
  # isolated subnet cannot have.
  ingress_controller_scope = "internal"
}
```

~> **No public address can be attached in an isolated subnet.** `apiserver_public_ip_id`,
`ingress_public_ip_id` and `ingress_controller_scope = "external"` all require a public address, and
the platform rejects attaching one to a resource of a subnet with `snat = false`. Reach such a
cluster over a private path — [`ccp_vpn_gateway`](vpn_gateway.md), [`ccp_bastion`](bastion.md), or a
peering — or set `snat = true` on the subnet.

~> Everything the workloads pull must be reachable without leaving the network. A private
[`ccp_registry`](registry.md) exposed inside the network covers images; anything else fetched from
the internet at runtime will not resolve.

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
  plan            = "standard" # HA: redundant apiserver frontend with automatic failover

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
    disk_gb     = 60        # optional — defaults to the plan's disk size
  }

  tags = ["k8s", "env:prod"]
}
```

## Example Usage — automatic updates and SSO

```hcl
resource "ccp_k8s_cluster" "prod" {
  name            = "prod-cluster"
  region          = "RNN"
  vpc_id          = ccp_vpc.main.id
  vnet_id         = ccp_vnet.workers.id
  k8s_version     = "v1.34.8"
  os_template_key = "kube-v1-34-8"
  plan            = "pro"

  # Only the fields written are managed; the others keep the platform value.
  maintenance = {
    auto_patch   = true
    auto_mineure = false
    fenetre = {
      jours        = [6, 7] # Saturday and Sunday
      heure_debut  = "03:00"
      duree_heures = 6
      fuseau       = "Europe/Paris"
    }
    dates_gel = [{ debut = "2026-12-20", fin = "2027-01-03" }]
  }

  # Sign-in with the organisation's identity provider (kubeconfig mode=sso).
  oidc = {
    issuer_url   = "https://sso.example.com/realms/acme"
    client_id    = "kubernetes" # public client, no secret
    extra_scopes = ["profile"]
  }

  initial_pool {
    name        = "default"
    plan        = "small"
    replicas    = 3
    k8s_version = "v1.34.8" # raise it after (or with) k8s_version to upgrade the pool
  }
}
```

## Kubernetes versions and upgrades

- `k8s_version` is the **desired** version of the control plane, written `X.Y.Z` with an optional `v` (anything else is refused at validation). Raising it calls the platform's control-plane upgrade (one minor at a time) and waits for its end; if an upgrade is already running (an automatic patch for instance), the provider waits for it **first** and compares afterwards — nothing is sent when the target is already reached. An automatic upgrade paused until the next maintenance window is reported as an error instead of being waited for.
- **A failed upgrade** is an error of the apply, with the platform's message (the cluster goes `status = "error"`). The platform keeps the requested version, so the next plan still proposes it, and **the next apply relaunches it** (same target) and waits again; the relaunch is reported as a warning. A platform that does not relaunch yet (before cetic-cloud-platform#2146) answers with an error saying so: relaunch it from the console, then apply. The provider never records a failed target as reached. The same applies to the node pools.
- The platform also moves versions **up** on its own: automatic patches inside the maintenance window (`maintenance.auto_patch`, on by default, and a patch forced 30 days after its release), upgrades from the console. The provider never **downgrades**: while the cluster runs the configured version or a higher one, `k8s_version` stays as written and no diff is shown. The version actually running is `current_k8s_version`. A configured value below it is recorded with a warning and nothing is called.
- Node pools are upgraded separately: `initial_pool.k8s_version` here, `k8s_version` on [`ccp_k8s_node_pool`](k8s_node_pool.md). The platform's pool upgrade replaces the nodes one at a time (drains respect the PodDisruptionBudgets); the target must be `<=` the control plane and of the same minor. A pool without a configured version keeps the version it runs.
- The platform rewrites `os_template_key` on a version upgrade (`kube-vX-Y-Z`); the provider keeps the configured value, so an upgrade never plans a replacement. A state refreshed by an earlier provider holds the rewritten key: moving between two keys of that `kube-vX-Y-Z` form is planned **in place** and applied without any call (no replacement); any other change of `os_template_key` still forces a replacement — including a configured key outside that form (e.g. `clks-capi-debian-13`) facing a rewritten key in such a state: the state keeps no trace of the key configured before, so it cannot be told apart from a real change of template. Write the key as the platform reports it (`kube-vX-Y-Z`) before upgrading the provider in that case. Such a state also holds the requested version without `v` (`1.34.9`): the first apply records the configured `k8s_version` (a warning when it is below the running version), without calling the platform.

## Argument Reference

### Required

- `name` - Name of the Kubernetes cluster.
- `region` - (Forces new resource) Region where the cluster is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Forces new resource) UUID of the VPC.
- `vnet_id` - (Forces new resource) UUID of the VNet where worker nodes attach. An **isolated** subnet (`snat = false`) is accepted — see the isolated-network example above for what it rules out.
- `k8s_version` - Desired Kubernetes version of the control plane (e.g. `"v1.34.8"`). Raising it upgrades the control plane only, in place (see [Kubernetes versions and upgrades](#kubernetes-versions-and-upgrades)); a value below the running version is never applied. Must match an `os_template_key` available in the region — list available versions via the [`ccp_k8s_templates`](../data-sources/k8s_templates.md) data source.
- `os_template_key` - (Forces new resource) OS template key for worker nodes (e.g. `"kube-v1-34-8"`). Must match the chosen `k8s_version` and be available in the region. The platform rewrites it on a version upgrade; the provider keeps the configured value (no replacement).

### Optional — cluster topology

- `display_name` - Display name shown in the console. Defaults to `name`.
- `os_image` - (Forces new resource) Operating-system family for the cluster nodes. One of `flatcar`, `ubuntu`, `rocky9`. Defaults to `flatcar` when omitted. Changing the node OS forces destroy + recreate.
- `plan` - Plan of the control plane:
    * `starter` (default) — free, single apiserver frontend (no HA), 1 control-plane replica, up to 10 nodes, etcd volume 2 GiB.
    * `standard` — redundant apiserver frontend (primary + secondary) with automatic failover on a floating address, 2 control-plane replicas, up to 50 nodes, etcd volume 5 GiB.
    * `pro` — like `standard` with 3 control-plane replicas, up to 150 nodes, etcd volume 10 GiB.

    `standard` ↔ `pro` is changed **in place**; the provider waits for the cluster to be `active` first, then watches the change for at least 2 minutes — longer while the cluster does not report the new plan yet, up to `timeouts.update` (default `30m`): the platform applies it asynchronously and may cancel it (node IP block that can no longer be extended) — the apply then fails with the platform's reason instead of proposing the change again at every plan. `starter` ↔ a paid plan **forces destroy + recreate** (the apiserver frontend changes topology; the platform refuses it in place). When `plan` depends on a value known only after apply, no replacement can be planned: the plan shows a warning and, if the value turns out to cross `starter` ↔ paid, **the apply fails** before changing anything (Terraform reports an inconsistent final plan, the change needing a new cluster) — set `plan` to a known value or recreate with `terraform apply -replace`. The provider also refuses such a change at apply as a safety net. The node limit is the sum, per pool, of `max(replicas, max_size)`. Plans and prices: `GET /v1/k8s/clusters/plans`.
- `tier` - **Deprecated**, use `plan`. `dev` is `plan = "starter"`, `prod` is a paid plan (`standard`, or `pro` when the cluster already is). Optional and computed: it always reads back as the tier of `plan`. A configuration that sets only `tier` keeps working without a diff (`tier = "prod"` on an existing cluster plans no change, even if the cluster was moved to `pro`). Setting both `plan` and `tier` with different topologies is an error.
- `pod_cidr` - (Forces new resource) Pod IP range. Default `"10.244.0.0/16"`.
- `service_cidr` - (Forces new resource) Service IP range. Default `"10.96.0.0/12"`.
- `tags` - List of free-form tags (max 60, max 50 chars each).
- `initial_pool` - Block describing the worker pool created with the cluster. Attributes:
    * `name` - (Forces new resource) Pool name. Default `"default"`.
    * `plan` - (Forces new resource) Instance plan (`nano` … `xlarge`). Default `"small"`.
    * `replicas` - Worker count. Mutable in-place (rolling).
    * `k8s_version` - (Optional) Desired Kubernetes version of the worker nodes in the initial pool, in `vX.Y.Z` format (e.g. `v1.33.4`). Omit to keep the version the pool runs (the control-plane version at creation). Raising it upgrades the pool in place through the platform's pool upgrade and waits for the end; the target must be `<=` the control-plane version and of the same minor. Since a control-plane upgrade no longer moves the pools, raise it after (or in the same apply as) `k8s_version`. A value below the version the pool runs is not applied (no downgrade, warning).
    * `disk_gb` - (Optional, Forces new resource) Root disk size in GB of every node in the initial pool. When omitted the platform picks 20 GB (or the plan's minimum disk if higher). No resize endpoint exists for node pools, so changing it forces destroy + recreate of the cluster (like `name`/`plan`).
    * `labels` - Map of Kubernetes labels applied to the pool's nodes (parity with `ccp_k8s_node_pool.labels`). Mutable in-place.
    * `taints` - Set of Kubernetes taints (`{ key, value?, effect }`, `effect` ∈ `NoSchedule`/`PreferNoSchedule`/`NoExecute`) applied to the pool's nodes (parity with `ccp_k8s_node_pool.taints`). Mutable in-place.
    * `min_size` / `max_size` - Cluster autoscaler bounds (see *Optional — autoscaler*).

### Optional — apiserver exposure

The apiserver always has a private endpoint (auto-allocated from the VNet CIDR or pinned via `apiserver_internal_ip`). It can additionally be exposed publicly via `apiserver_public_ip_id`.

- `apiserver_internal_ip` - (Forces new resource) Pinned private IP for the apiserver. Defaults to auto-allocated within the VNet.
- `apiserver_public_ip_id` - UUID of a public IP attached to the apiserver (public kubeconfig). **Mutable** — set the UUID to attach, remove it (`null`) to detach, change it to rotate, all **without recreating the cluster**. Works both at create time and later. The IP must be in the same region as the cluster and be a routed public IP. **Not available in an isolated subnet**: the platform refuses to attach a public address in a subnet whose `snat` is `false`.

> **Migration (provider v3.0.0)**: the former separate `public_ip_id` attribute is removed — `apiserver_public_ip_id` is now the single, mutable knob for the apiserver public IP. If you used `public_ip_id`, rename it to `apiserver_public_ip_id`.

### Optional — ingress controller

CETIC Cloud Kubernetes Service (CCKS) ships with an ingress controller deployed in-cluster at bootstrap. The combination of `ingress_controller_class` × `ingress_controller_scope` determines the data path and the type of IP to pre-reserve.

- `ingress_controller_enabled` - Deploy an ingress controller at bootstrap. Mutable. Default `true`.
- `ingress_controller_class` - Implementation:
    * `incluster` (default) — Cilium-based ingress deployed inside the cluster. The IP is advertised by the workers themselves on the VNet (internal scope) or routed through CETIC Cloud's regional network (external scope). No extra load balancer to provision.
    * `managed` — A dedicated CETIC Cloud load balancer (HA pair with automatic failover) is provisioned in front of the cluster. Useful when you need L4 features like proxy protocol or per-listener TLS termination decoupled from the cluster.
- `ingress_controller_scope` - Network reachability:
    * `internal` (default) — Private IP on the VNet. Reachable only from inside the VPC (or via VPC peering).
    * `external` — Public IP routed by the platform.
- `ingress_public_ip_id` - UUID of a pre-reserved public IP to use for the ingress (only meaningful with `scope = "external"`). If empty, the API auto-allocates one from the regional pool.
- `ingress_internal_ip` - Pre-reserved private VNet IP for the ingress (only meaningful with `scope = "internal"`). If empty, the API auto-allocates one from the VNet CIDR. The IP must be inside the VNet's range and outside the DHCP range.

#### Ingress controller — the four combinations

| `class` | `scope` | Use case | IP source | Characteristics |
|---------|---------|----------|-----------|-----------------|
| `incluster` | `internal` | Workloads only reached from inside the VPC (intranet, B2B over VPC peering) | Auto-allocated from VNet CIDR, or pin via `ingress_internal_ip` | Handled by the Cilium ingress controller deployed inside the cluster — minimal cost, no extra load balancer |
| `incluster` | `external` | Public services with the lowest possible cost | Auto-allocated, or pin via `ingress_public_ip_id` (region must match cluster) | The public IP routes to the workers through CETIC Cloud's regional network — no external load balancer to provision |
| `managed` | `internal` | Internal services that need L4 features (proxy protocol, custom listeners) | Auto-allocated, or pin via `ingress_internal_ip` | Dedicated load balancer, HA pair with automatic failover on a floating address — TLS and L4 features decoupled from the cluster |
| `managed` | `external` | Public services that need an external load balancer | Auto-allocated, or pin via `ingress_public_ip_id` | Dedicated load balancer exposed on the internet — TLS termination and full L4 control at a tier separate from the cluster |

### Optional — automatic updates (`maintenance`)

Optional and computed: without it, the platform settings are read back and left alone (changes made in the console included). With it, **only the fields written are managed** — removing a field, or the whole attribute, keeps the current value. The provider sends the written fields only when they differ from the state.

- `auto_patch` - Apply new patches of the current minor automatically, inside the window. Platform default `true`. Even when `false`, a patch is applied 30 days after its release.
- `auto_mineure` - Move to the next minor version automatically. Default `false`.
- `auto_majeure` - Move to the next major version automatically (no effect while no 2.x exists). Default `false`.
- `fenetre` - Maintenance window, a whole block (all four fields required): an automatic update only starts inside it.
    * `jours` - ISO week days, `1` (Monday) to `7` (Sunday). Default `[2, 3, 4]`.
    * `heure_debut` - Local start time, `HH:MM`. Default `"02:00"`.
    * `duree_heures` - Duration in hours, 4 to 24. Default `4`.
    * `fuseau` - IANA time zone. Default `"Europe/Paris"`.
- `dates_gel` - Freeze periods (at most 12, 31 days each, bounds included, `YYYY-MM-DD` in the window's time zone): no window starting on those days is used. `{ debut, fin }` objects; replaces the whole list, `[]` empties it.
- `personnalise` - (Computed) `false` while the cluster follows the platform defaults.

### Optional — SSO (`oidc`)

Users sign in to the cluster with the organisation's identity provider (`kubeconfig` in `mode=sso`, through kubelogin). Authoritative: set, it is the configuration of the cluster; **removed, the SSO is removed**. **A change restarts the apiserver** (a short API cut on the `starter` plan): the provider only sends it when it actually changes once normalised like the platform does (surrounding spaces, duplicate scopes, PEM layout). The certificate kubeconfig remains the cluster-admin access. Requires an active cluster: the provider waits for it (an automatic maintenance may be running). At creation, a configuration the platform refuses (unreachable issuer, invalid CA…) is a **warning**, not an error — the cluster is kept, not tainted — and the next apply retries the SSO in place; the same holds for `maintenance`.

- `issuer_url` - (Required) Exact `iss` of the tokens: `https://`, port 443, public host, no query nor fragment.
- `client_id` - (Required) **Public** client of the identity provider (PKCE / device flow, no secret) — the expected audience.
- `username_claim` - Claim giving the Kubernetes user name. Default `"email"`.
- `username_prefix` - Prefix of the user name (`oidc:alice@example.com`); `""` for none. Default `"oidc:"`.
- `groups_claim` - Claim giving the groups; `""` for no groups (RBAC by user name only). Default `"groups"`.
- `groups_prefix` - Prefix of every group; `""` for none. Default `"oidc:"`.
- `extra_scopes` - Scopes asked for besides `openid` (at most 10). Default none.
- `ca_pem` - PEM certificates of the identity provider's CA, when it is not signed by a public CA.

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
- `status` - Current status. Possible values: `creating`, `provisioning`, `active`, `updating` (version upgrade running), `error` (e.g. after a failed upgrade — the next apply relaunches it), `deleting`.
- `current_k8s_version` - Kubernetes version the control plane actually runs: the platform's `k8s_version_effective`, updated at the end of an upgrade (may be above `k8s_version` after an automatic patch). On a platform that does not report it yet, the version last requested — the target during an upgrade, and also after a failed one (check `status`).
- `etcd_volume_gb` - Effective etcd volume (GiB): the plan's at creation, raised when the plan goes up, never reduced.
- `node_ip_block_size` - Size of the node IP block allocated in the VNet (`null` until allocated).
- `kubeconfig` - (Sensitive) Kubeconfig file content (YAML) for accessing the cluster with `kubectl`. Store securely.
- `endpoint` - Kubernetes API server endpoint URL (e.g. `https://10.0.1.5:6443`).

### Apiserver

- `public_ip_address` - Public IP address of the apiserver, if attached. Empty otherwise.

### Ingress

- `ingress_public_ip_address` - Effective public IP of the ingress controller (Computed). Populated once the IP is reserved + bound — `scope = "external"` only.

### Apiserver frontend HA (`plan` = `standard` or `pro`)

This attribute is populated on a paid plan and is `null` on `starter`.

- `proxy_vip_vnet` - Floating address on the VNet shared between the two apiserver frontends — this is the IP the kubeconfig points at when HA is active.

### Optional — waiting times (`timeouts`)

- `create` - Provisioning until `active`. Default `15m`.
- `update` - Each wait of an update. Defaults: `30m` for a running upgrade to end (or for an active cluster before an SSO or plan change), `45m` for the control-plane upgrade, `90m` for the upgrade of the initial pool. Set, the value applies to each of them.
- `delete` - Teardown until the cluster is gone. Default `20m`.

Go durations (`30m`, `1h30m`). Never sent to the platform: adding or changing them plans an update without any call.

## Import

Kubernetes clusters can be imported using their UUID:

```
terraform import ccp_k8s_cluster.prod <cluster_id>
```

`initial_pool` is never read back from the platform; at import it is rebuilt once from the oldest pool of the cluster (the one created with it): `name`, `plan`, `replicas`, `min_size`/`max_size`, `labels`, `taints` and `disk_gb`. Write the block with the same values. `k8s_version` is left unset (the pool keeps its version) and leaving `disk_gb` unset right after the import does not force a replacement. Set `os_template_key` and `k8s_version` as the platform reports them, or as written before — two keys of the `kube-vX-Y-Z` form never replace the cluster.

## See also

- [Public docs — `ccp_k8s_cluster` end-to-end examples](https://docs.cloud.cetic-group.com/terraform/ccp_k8s_cluster) — full HCL for the 4 ingress combinations, autoscaler patterns, node pool composition.
- [`ccp_k8s_node_pool`](k8s_node_pool.md) — additional worker pools.
- [`ccp_k8s_templates` data source](../data-sources/k8s_templates.md) — list available `os_template_key` + `k8s_version` pairs per region.
- [`managed/k8s-cluster` Terraform module](https://github.com/cetic-group/cetic-cloud-terraform-modules/tree/main/modules/managed/k8s-cluster) — composable wrapper with sensible defaults.
