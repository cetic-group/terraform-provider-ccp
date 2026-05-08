---
page_title: "ccp_db_ferretdb_instance Resource - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Manages a managed FerretDB (MongoDB-compatible) instance on CETIC Cloud Platform.
---

# ccp_db_ferretdb_instance (Resource)

Manages a managed MongoDB-compatible instance powered by FerretDB v2 (Apache 2.0 license). FerretDB v2 translates the MongoDB wire protocol to SQL, storing data in a managed PostgreSQL cluster with the **DocumentDB extension** (PG-DocumentDB image) — both running in the same isolated namespace. Compatible with all standard MongoDB drivers, tools, and the MongoDB shell.

~> **Note:** The `tier` argument is immutable — it determines the replica count at creation and cannot be changed. `dev` provisions 1 PG instance + 1 FerretDB pod (no HA); `prod` provisions 3 PG instances + 3 FerretDB pods (HA inter-nodes via topologySpreadConstraints, RollingUpdate zero-downtime). To migrate tiers, create a new instance and restore data.

~> **Note:** Database provisioning is asynchronous. The provider polls until the instance reaches `active` status. Provisioning includes both the FerretDB layer and its backing PostgreSQL cluster (CNPG operator) with the DocumentDB extension installed via `postInitSQL`.

~> **Note:** Connection string format is `mongodb://postgres:<password>@<endpoint>:27017/postgres?authSource=admin`. The user is always `postgres` (PG superuser) and the database is `postgres` (where DocumentDB stores MongoDB-shaped tables). FerretDB v2 bridges MongoDB SCRAM-SHA-256 auth to PG SCRAM via the superuser secret.

~> **Note:** Database provisioning is asynchronous. The provider polls until the instance reaches `active` status. Provisioning includes both the FerretDB layer and its backing PostgreSQL cluster.

## Example Usage

```hcl
resource "ccp_db_ferretdb_instance" "app_docs" {
  name           = "app-mongodb"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  tier           = "prod"
  engine_version = "2"
  tags           = ["database", "mongodb-compat", "env:prod"]
}

output "mongo_uri" {
  # admin_username = "postgres" et admin_database = "postgres" sont
  # toujours fixes pour FerretDB v2 (le superuser PG est utilisé pour
  # auth, et DocumentDB stocke ses collections dans la DB `postgres`).
  value     = "mongodb://${ccp_db_ferretdb_instance.app_docs.admin_username}:${ccp_db_ferretdb_instance.app_docs.admin_password}@${ccp_db_ferretdb_instance.app_docs.endpoint_host}:${ccp_db_ferretdb_instance.app_docs.endpoint_port}/${ccp_db_ferretdb_instance.app_docs.admin_database}?authSource=admin"
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the FerretDB instance.
- `region` - (Required, Forces new resource) Region where the instance is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC for internal network connectivity.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the database endpoint is accessible.
- `plan` - (Required, Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.

### Optional

- `engine_version` - (Optional, Forces new resource) FerretDB major version to deploy (e.g. `"2"` for FerretDB v2 with DocumentDB extension). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version (`2`).
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the instance.
- `status` - Current status. Possible values: `provisioning`, `active`, `error`.
- `endpoint_host` - Hostname or IP address for connecting to FerretDB within the VNet (MongoDB wire protocol). Routes to the FerretDB Service (port 27017), **not** the PG cluster (which is namespace-internal on port 5432).
- `endpoint_port` - TCP port for MongoDB connections (always `27017`).
- `admin_username` - Always `postgres` for FerretDB v2 (the PG superuser used for SCRAM-SHA-256 auth bridging).
- `admin_database` - Always `postgres` for FerretDB v2 (DocumentDB extension stores its MongoDB-shaped tables there).
- `admin_password` - (Sensitive) Administrator password (used by both the FerretDB pod to connect to the PG backend AND by the MongoDB client connection string).

## Import

FerretDB instances can be imported using their UUID:

```
terraform import ccp_db_ferretdb_instance.app_docs <instance_id>
```
