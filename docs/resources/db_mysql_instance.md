---
page_title: "ccp_db_mysql_instance Resource - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Manages a managed MySQL instance (MariaDB) on CETIC Cloud Platform.
---

# ccp_db_mysql_instance (Resource)

Manages a managed MySQL-compatible instance backed by MariaDB. Instances run on the shared regional Kubernetes workload cluster. The `prod` tier uses Galera Cluster for synchronous multi-master replication, ensuring no data loss on node failure. Compatible with all MySQL drivers and tools.

~> **Note:** This engine is currently in preview. The `tier` argument is immutable — it determines the replica count at creation and cannot be changed. `dev` provisions 1 replica (no HA); `prod` provisions 3 replicas with Galera replication. To migrate from `dev` to `prod`, create a new instance from a backup.

~> **Note:** Database provisioning is asynchronous. The provider polls until the instance reaches `active` status.

## Example Usage

```hcl
resource "ccp_db_mysql_instance" "app_db" {
  name           = "app-mysql"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  tier           = "prod"
  engine_version = "11"
  tags           = ["database", "mysql", "env:prod"]
}

output "mysql_dsn" {
  value     = "mysql://${ccp_db_mysql_instance.app_db.admin_username}:${ccp_db_mysql_instance.app_db.admin_password}@${ccp_db_mysql_instance.app_db.endpoint_host}:${ccp_db_mysql_instance.app_db.endpoint_port}/${ccp_db_mysql_instance.app_db.admin_database}"
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the MySQL instance.
- `region` - (Required, Forces new resource) Region where the instance is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC for internal network connectivity.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the database endpoint is accessible.
- `plan` - (Required, Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.

### Optional

- `engine_version` - (Optional, Forces new resource) MariaDB major version to deploy (e.g. `"10"`, `"11"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the instance.
- `status` - Current status. Possible values: `provisioning`, `active`, `error`.
- `endpoint_host` - Hostname or IP address for connecting to the database within the VNet.
- `endpoint_port` - TCP port for MySQL connections (typically `3306`).
- `admin_username` - Database administrator username.
- `admin_database` - Name of the default database created at provisioning.
- `admin_password` - (Sensitive) Administrator password.

## Import

MySQL instances can be imported using their UUID:

```
terraform import ccp_db_mysql_instance.app_db <instance_id>
```
