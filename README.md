# CETIC Cloud Platform Terraform Provider

Terraform provider for CETIC Cloud — sovereign cloud by CETIC Group.

> **`provider "ccp"` and `provider "cetic-cloud-platform"` are the SAME provider.**
>
> The provider is published on the Terraform Registry as
> `cetic-group/cetic-cloud-platform`. The local name you choose in
> `required_providers` determines the HCL block name :
>
> - If you alias to `ccp` (recommended — shorter), you write `provider "ccp"`.
> - If you skip the alias, the default local name is `cetic-cloud-platform`,
>   so you write `provider "cetic-cloud-platform"`.
>
> **All examples in this README and on the Registry use `ccp`** — copy the
> `required_providers` block in section 3 below as-is to keep them working
> without modification.
>
> Resources always start with the prefix `ccp_` regardless of which local
> name you picked (e.g. `ccp_vpc`, `ccp_vm_instance`, `ccp_db_pg_instance`).

> **Status — v0.8.1**
>
> **30 resources + 11 data sources** implemented. Highlights : containers
> (instance + scale set + snapshot), virtual machines (instance + scale
> set + snapshot), managed Kubernetes clusters + node pools, **managed
> databases — PostgreSQL / MySQL-compatible / Redis-compatible (Valkey) /
> MongoDB-compatible (FerretDB v2)**, public IPs, VPCs / VNets / firewall
> / private IP reservations / VPC peering, load balancers, block volumes,
> object storage buckets + S3 keys, SSH/API keys, organizations + members,
> custom templates (snapshot a running instance into a reusable template),
> support tickets, quota requests. Catalog data sources avoid hardcoding
> identifiers. Full roadmap in [`NOTES.md`](./NOTES.md).

---

## Quick start

### 1. Get an API key

In the CETIC Cloud console, go to **Security → API keys** and create one with
`write` scope. Tokens look like `ccp_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`
and are shown **only once** at creation.

### 2. Configure the environment

```bash
export CCP_API_KEY="ccp_live_..."
export CCP_API_URL="https://api.cloud.cetic-group.com"   # optional, this is the default
```

### 3. Declare the provider

```hcl
terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 0.8.1"
    }
  }
}

provider "ccp" {}
```

> **Provider name** — the registry source is `cetic-group/cetic-cloud-platform`.
> All examples in this README use the **local alias `ccp`** (declared in
> `required_providers` above). Without aliasing, the default local name is
> `cetic-cloud-platform` — both work, but copy the `required_providers`
> block as-is to keep examples consistent.

A full working example (SSH key, VPC, two VNets, region listing) lives in
[`examples/basic/main.tf`](./examples/basic/main.tf).

---

## Resources

| Category | Name | Notes |
|---|---|---|
| Identity | `ccp_ssh_key` | No update — all changes force replacement. |
| Identity | `ccp_api_key` | API token mgmt. |
| Identity | `ccp_organization` / `ccp_org_member` | Multi-tenant orgs + member roles. |
| Network | `ccp_vpc` | Async create/delete, polls until `active`. |
| Network | `ccp_vnet` | Nested under VPC. PATCH supports `name`, `snat`. |
| Network | `ccp_vnet_firewall_rule` | Per-VNet rules. |
| Network | `ccp_vnet_ip_reservation` / `ccp_vnet_peering` / `ccp_vpc_peering` | IP reservations + peering intra/inter-VPC. |
| Network | `ccp_public_ip` / `ccp_ipaas_pool` | Public IPs + BYOIP edge pools. |
| Network | `ccp_load_balancer` | Highly available with floating VIP, automatic failover. |
| Compute | `ccp_container_instance` / `ccp_container_scale_set` / `ccp_container_snapshot` | Linux containers — fast boot, low overhead. |
| Compute | `ccp_vm_instance` / `ccp_vm_scale_set` / `ccp_vm_snapshot` | Full virtual machines — kernel isolation. |
| Compute | `ccp_k8s_cluster` / `ccp_k8s_node_pool` | Managed Kubernetes clusters with auto-scaling node pools. |
| Compute | `ccp_custom_template` | Snapshot a running container/VM into a reusable template scoped to your organization. |
| Storage | `ccp_block_volume` | Resizable block storage. `size_gb` can grow; attach/detach via `attached_to_*`. |
| Storage | `ccp_object_bucket` / `ccp_object_storage_key` | S3-compatible object storage buckets + scoped access keys. |
| Database | `ccp_db_pg_instance` / `ccp_db_mysql_instance` / `ccp_db_valkey_instance` / `ccp_db_ferretdb_instance` | Managed PostgreSQL / MySQL-compatible / Redis-compatible (Valkey) / MongoDB-compatible (FerretDB v2). |
| Support | `ccp_support_ticket` / `ccp_quota_request` | Ticketing + quota self-service. |

## Data sources

| Name | Notes |
|---|---|
| `ccp_regions` | Active regions (RNN/PAR/ABJ). |
| `ccp_organizations` | Orgs accessible to the current API key's tenant. |
| `ccp_lxc_templates` | Container template catalog (resolve `key` for `ccp_container_instance.template`). |
| `ccp_qemu_templates` | Virtual machine template catalog (resolve `key` for `ccp_vm_instance.template`). |
| `ccp_k8s_templates` | Kubernetes node OS template catalog. |
| `ccp_db_plans` | Database plan catalog, filterable by `engine`. |
| `ccp_db_engine_versions` | Active database engine versions, filterable by `engine`. |
| `ccp_db_pg_credentials` | Admin credentials of a PostgreSQL instance (sensitive). |
| `ccp_db_mysql_credentials` | Admin credentials of a MySQL-compatible instance (sensitive). |
| `ccp_db_ferretdb_credentials` | Admin credentials of a FerretDB v2 instance (sensitive). |
| `ccp_db_valkey_credentials` | Admin password of a Valkey (Redis-compatible) instance (sensitive). |

## Multi-organization

Each CETIC Cloud API key is **bound to a single organization**
(`api_keys.org_id` in the API). The provider does **not** offer an
`organization` argument — to target a different org, use a different API
key via Terraform's provider aliases:

```hcl
provider "ccp" {
  # default — reads CCP_API_KEY (org "prod")
}

provider "ccp" {
  alias    = "staging"
  api_key  = var.ccp_staging_key   # org "staging"
}

resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

resource "ccp_vpc" "staging" {
  provider = ccp.staging
  name     = "staging"
  region   = "RNN"
}
```

To discover which orgs your account can access (useful for picking the
right API key), use the data source:

```hcl
data "ccp_organizations" "all" {}

output "accessible_orgs" {
  value = [for o in data.ccp_organizations.all.organizations : {
    id         = o.id
    name       = o.name
    is_default = o.is_default
  }]
}
```

---

## Provider configuration

| Argument   | Env var       | Default                       | Description                    |
|------------|---------------|-------------------------------|--------------------------------|
| `api_key`  | `CCP_API_KEY`  | _(required)_                          | API key, format `ccp_live_*`.        |
| `endpoint` | `CCP_API_URL` | `https://api.cloud.cetic-group.com`   | Base URL of the CETIC Cloud API.    |

```hcl
provider "ccp" {
  # api_key  = "ccp_live_..."             # prefer the env var
  # endpoint = "https://api.cloud.cetic-group.com"
}
```

---

## Local development

The provider is built with Go 1.22+ and the Terraform Plugin Framework.

```bash
# Build the binary in the current directory
make build

# Build + install into the local Terraform plugin cache so a sibling
# terraform init can pick it up:
#   ~/.terraform.d/plugins/registry.terraform.io/cetic-group/cetic-cloud-platform/0.3.0/<os>_<arch>/
make install

# Other helpers
make fmt    # gofmt -w .
make vet    # go vet ./...
make tidy   # go mod tidy
make clean  # remove the local binary + the installed plugin
```

After `make install`, in any example directory:

```bash
cd examples/basic
terraform init
terraform plan
```

Terraform will discover the locally installed build and skip the registry.

---

## License

MIT.
