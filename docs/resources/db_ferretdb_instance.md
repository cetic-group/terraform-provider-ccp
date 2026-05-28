---
page_title: "ccp_db_ferretdb_instance Resource - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Manages a managed FerretDB (MongoDB-compatible) instance on CETIC Cloud Platform.
---

# ccp_db_ferretdb_instance (Resource)

Manages a managed MongoDB-compatible instance powered by FerretDB v2 (Apache 2.0 license). FerretDB v2 translates the MongoDB wire protocol to SQL, storing data in a managed PostgreSQL cluster with the **DocumentDB extension**. Compatible with all standard MongoDB drivers, tools, and the MongoDB shell.

~> **Note:** `replicas` is set at creation and immutable — `1` provisions a single FerretDB pod with a single PG backend (`tier = "dev"`), `3` provisions HA with topology spread on both layers (`tier = "prod"`). To migrate from dev to prod, create a new instance and restore data.

~> **Note:** Provisioning is asynchronous. The provider polls until the instance reaches `active` status. Provisioning sets up both the FerretDB layer and its backing PostgreSQL cluster.

~> **Note:** Connection string format is `mongodb://postgres:<password>@<endpoint_vnet_ip>:27017/postgres?authSource=admin`. The username is always `postgres` (the PG superuser used for SCRAM-SHA-256 auth bridging) and the database is always `postgres` (where DocumentDB stores MongoDB-shaped tables).

## Example Usage

```hcl
resource "ccp_db_ferretdb_instance" "app_docs" {
  name           = "app-mongodb"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  storage_gb     = 50
  replicas       = 3 # HA — `tier` will be computed as "prod"
  engine_version = "2"
  tags           = ["database", "mongodb-compat", "env:prod"]
}

output "mongo_endpoint" {
  value = "mongodb://${ccp_db_ferretdb_instance.app_docs.admin_username}@${ccp_db_ferretdb_instance.app_docs.endpoint_vnet_ip}:${ccp_db_ferretdb_instance.app_docs.endpoint_port}/${ccp_db_ferretdb_instance.app_docs.admin_database}?authSource=admin"
}
```

Retrieve the admin password through the dedicated CLI / API endpoint (not exposed as a Terraform attribute):

```bash
cetic db ferretdb credentials <instance_id>
```

## Argument Reference

### Required

- `name` - Name of the FerretDB instance.
- `region` - (Forces new resource) Region. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Forces new resource) UUID of the VPC.
- `vnet_id` - (Forces new resource) UUID of the VNet where the endpoint is accessible.
- `plan` - (Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `storage_gb` - Persistent storage size in GB (for the backing PG cluster).

### Optional

- `replicas` - (Forces new resource) Replica count. `1` (default) = single FerretDB pod + single PG. `3` = HA on both layers. Sets `tier` accordingly. Default `1`.
- `engine_version` - (Forces new resource) FerretDB major version (e.g. `"2"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the instance.
- `status` - Current status. One of: `provisioning`, `active`, `error`.
- `tier` - Derived from `replicas` — `dev` (1 replica) or `prod` (3 replicas, HA).
- `endpoint_vnet_ip` - Private IP within the VNet for MongoDB connections (routes to the FerretDB Service on port 27017, NOT the backing PG cluster).
- `endpoint_port` - TCP port for MongoDB connections (always `27017`).
- `admin_username` - Always `postgres` for FerretDB v2 (the PG superuser used for SCRAM-SHA-256 auth bridging).
- `admin_database` - Always `postgres` for FerretDB v2 (DocumentDB extension stores its MongoDB-shaped tables there).
- `cpu_millicores` - CPU allocation in millicores. Derived from `plan`.
- `memory_mb` - Memory allocation in MB. Derived from `plan`.

## Import

FerretDB instances can be imported using their UUID:

```
terraform import ccp_db_ferretdb_instance.app_docs <instance_id>
```
