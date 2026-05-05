---
page_title: "ccp_db_ferretdb_instance Resource - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Manages a managed FerretDB (MongoDB-compatible) instance on CETIC Cloud Platform.
---

# ccp_db_ferretdb_instance (Resource)

Manages a managed MongoDB-compatible instance powered by FerretDB (Apache 2.0 license). FerretDB translates the MongoDB wire protocol to SQL, storing data in a managed PostgreSQL backend — both running in the same isolated namespace. Compatible with all standard MongoDB drivers, tools, and the MongoDB shell.

~> **Note:** This engine is currently in preview. The `tier` argument is immutable — it determines the replica count at creation and cannot be changed. `dev` provisions 1 replica (no HA); `prod` provisions 3 replicas for HA. To migrate tiers, create a new instance and restore data.

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
  engine_version = "1"
  tags           = ["database", "mongodb-compat", "env:prod"]
}

output "mongo_uri" {
  value     = "mongodb://${ccp_db_ferretdb_instance.app_docs.admin_username}:${ccp_db_ferretdb_instance.app_docs.admin_password}@${ccp_db_ferretdb_instance.app_docs.endpoint_host}:${ccp_db_ferretdb_instance.app_docs.endpoint_port}/${ccp_db_ferretdb_instance.app_docs.admin_database}"
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
- `tier` - (Required, Forces new resource) Service tier. One of: `dev` (1 replica, no HA), `prod` (3 replicas, HA).

### Optional

- `engine_version` - (Optional, Forces new resource) FerretDB major version to deploy (e.g. `"1"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the instance.
- `status` - Current status. Possible values: `provisioning`, `active`, `error`.
- `endpoint_host` - Hostname or IP address for connecting to FerretDB within the VNet (MongoDB wire protocol).
- `endpoint_port` - TCP port for MongoDB connections (typically `27017`).
- `admin_username` - Database administrator username.
- `admin_database` - Name of the default database created at provisioning.
- `admin_password` - (Sensitive) Administrator password.

## Import

FerretDB instances can be imported using their UUID:

```
terraform import ccp_db_ferretdb_instance.app_docs <instance_id>
```
