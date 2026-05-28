---
page_title: "ccp_db_pg_instance Resource - cetic-cloud-platform"
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

## Import

PostgreSQL instances can be imported using their UUID:

```
terraform import ccp_db_pg_instance.app_db <instance_id>
```
