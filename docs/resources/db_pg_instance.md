---
page_title: "ccp_db_pg_instance Resource - ccp"
subcategory: "Databases"
description: |-
  Manages a managed PostgreSQL instance on CETIC Cloud Platform.
---

# ccp_db_pg_instance (Resource)

Manages a managed PostgreSQL instance on CETIC Cloud Platform. The instance runs on a shared regional Kubernetes workload cluster, isolated in its own namespace with NetworkPolicies and ResourceQuotas. Data is persisted on high-performance block storage.

~> **Note:** `replicas` is set at creation and immutable — `1` provisions a single instance (`tier = "dev"`), `3` provisions an HA cluster with pod anti-affinity (`tier = "prod"`). To migrate from dev to prod, create a new instance and restore from a snapshot.

~> **Note:** Provisioning is asynchronous. The provider polls until the instance reaches `active` status. Single replica: ~3 minutes. HA cluster: ~5 minutes.

## Example Usage

```hcl
resource "ccp_db_pg_instance" "app_db" {
  name           = "app-postgres"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  storage_gb     = 50
  replicas       = 3 # HA cluster — `tier` will be computed as "prod"
  engine_version = "16"
  tags           = ["database", "postgres", "env:prod"]
}

output "db_endpoint" {
  value = "${ccp_db_pg_instance.app_db.admin_username}@${ccp_db_pg_instance.app_db.endpoint_vnet_ip}:${ccp_db_pg_instance.app_db.endpoint_port}/${ccp_db_pg_instance.app_db.admin_database}"
}
```

### Extensions

```hcl
data "ccp_db_pg_extensions" "pg16" {
  version = 16
}

resource "ccp_db_pg_instance" "geo_db" {
  name       = "geo-postgres"
  region     = "RNN"
  vpc_id     = ccp_vpc.main.id
  vnet_id    = ccp_vnet.data.id
  plan       = "medium"
  storage_gb = 50

  # Dependencies are listed too: postgis_topology requires postgis.
  allowed_extensions = ["postgis", "postgis_topology", "pg_cron"]
  preload_libraries  = ["pg_cron"] # pg_cron must be preloaded
  cron_database      = "jobs"      # database where pg_cron runs its jobs
}
```

Then, as the owner of the database, run `CREATE EXTENSION postgis;`.

Retrieve the admin password through the dedicated CLI / API endpoint (it is intentionally **not** exposed as a Terraform attribute):

```bash
cetic db pg credentials <instance_id>
# or
curl -H "Authorization: Bearer $CCP_API_KEY" \
  https://api.cloud.cetic-group.com/v1/db/pg/<instance_id>/credentials
```

## Argument Reference

### Required

- `name` - Name of the PostgreSQL instance.
- `region` - (Forces new resource) Region. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Forces new resource) UUID of the VPC.
- `vnet_id` - (Forces new resource) UUID of the VNet where the endpoint is accessible.
- `plan` - (Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `storage_gb` - Persistent storage size in GB. Mutable in some plans (cf. backoffice settings), forces replacement otherwise.

### Optional

- `replicas` - (Forces new resource) Replica count. `1` (default) = single instance, `3` = HA cluster. Sets `tier` accordingly. Default `1`.
- `engine_version` - (Forces new resource) PostgreSQL major version (e.g. `"15"`, `"16"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - List of free-form tags (max 60, max 50 chars each).
- `allowed_extensions` - (Updated in place) Set of extensions from the catalog ([`ccp_db_pg_extensions`](../data-sources/db_pg_extensions.md)) that the database owner may then create with `CREATE EXTENSION`. List the extensions they depend on as well. Extensions that need no authorization (`direct_extensions` of the catalog, e.g. `pgcrypto`, `pg_trgm`) are not listed here: create them directly. Omitted: left unchanged; `[]`: none allowed.
- `preload_libraries` - (Updated in place) Set of libraries loaded at startup, required by some extensions (`pg_cron`, `pg_stat_statements`). **Changing this list restarts the instance**, one replica at a time: a single-replica instance is briefly unavailable. Omitted: left unchanged; `[]`: none.
- `cron_database` - (Updated in place) Database in which `pg_cron` runs scheduled jobs. Requires `pg_cron` in `preload_libraries`. Defaults to the initial database. Omitted: left unchanged.

## Extensions

Extensions are allowed on an instance, then created by the owner of a database:

- **Enabling**: extensions must first be enabled on the instance by support (`extensions_available = true`). Until then, extension settings cannot change. A new instance always starts without them: if they are set at creation, the instance is still created, with a warning, and the next `terraform apply` applies the settings once support has enabled extensions.
- **Plan-time checks**: the settings are checked against the catalog of the instance's major version. A missing dependency, a library to preload, an extension that needs no authorization, or `cron_database` without `pg_cron` fails the plan with a message naming the fix.
- **Apply**: `terraform apply` waits until the instance runs with the new settings (`extensions_status = "applied"`, up to 20 minutes).
- **Removing an extension** is refused while it is installed in a database: run `DROP EXTENSION` first, then remove it from `allowed_extensions`. The refusal names the databases concerned. It is also refused while a database of the instance does not accept connections.
- Only `allowed_extensions`, `preload_libraries` and `cron_database` change in place. Changing another non-replacing attribute (`storage_gb`, `tags`) fails the apply.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the instance.
- `status` - Current status. One of: `provisioning`, `active`, `error`.
- `tier` - Derived from `replicas` — `dev` (1 replica) or `prod` (3 replicas, HA).
- `endpoint_vnet_ip` - Private IP within the VNet for PostgreSQL connections.
- `endpoint_port` - TCP port for PostgreSQL connections (typically `5432`).
- `admin_username` - Default administrator username.
- `admin_database` - Name of the default database created at provisioning.
- `cpu_millicores` - CPU allocation in millicores (e.g. `2000` = 2 vCPU). Derived from `plan`.
- `memory_mb` - Memory allocation in MB. Derived from `plan`.
- `extensions_available` - Whether extensions can be allowed on this instance (enabled by support).
- `extensions_status` - `applied` when the instance runs with its extension settings, `pending` while a change is being applied.

## Import

PostgreSQL instances can be imported using their UUID:

```
terraform import ccp_db_pg_instance.app_db <instance_id>
```
