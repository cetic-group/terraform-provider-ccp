---
page_title: "ccp_db_mysql_instance Resource - ccp"
subcategory: "Databases"
description: |-
  Manages a managed MySQL-compatible instance (MariaDB) on CETIC Cloud Platform.
---

# ccp_db_mysql_instance (Resource)

Manages a managed MySQL-compatible instance backed by MariaDB. Instances run on the shared regional Kubernetes workload cluster, isolated per tenant. The HA configuration uses synchronous multi-master replication, ensuring no data loss on node failure. Compatible with all MySQL drivers and tools.

~> **Note:** `replicas` is set at creation and immutable — `1` provisions a single instance (`tier = "dev"`), `3` provisions an HA cluster (`tier = "prod"`). To migrate from dev to prod, create a new instance from a backup.

~> **Note:** Provisioning is asynchronous. The provider polls until the instance reaches `active` status.

## Example Usage

```hcl
resource "ccp_db_mysql_instance" "app_db" {
  name           = "app-mysql"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  storage_gb     = 50
  replicas       = 3 # HA cluster — `tier` will be computed as "prod"
  engine_version = "11"
  tags           = ["database", "mysql", "env:prod"]
}

output "mysql_endpoint" {
  value = "${ccp_db_mysql_instance.app_db.admin_username}@${ccp_db_mysql_instance.app_db.endpoint_vnet_ip}:${ccp_db_mysql_instance.app_db.endpoint_port}/${ccp_db_mysql_instance.app_db.admin_database}"
}
```

Retrieve the admin password through the dedicated CLI / API endpoint (not exposed as a Terraform attribute):

```bash
cetic db mysql credentials <instance_id>
```

## Argument Reference

### Required

- `name` - Name of the MySQL instance.
- `region` - (Forces new resource) Region. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Forces new resource) UUID of the VPC.
- `vnet_id` - (Forces new resource) UUID of the VNet where the endpoint is accessible.
- `plan` - (Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `storage_gb` - Persistent storage size in GB.

### Optional

- `replicas` - (Forces new resource) Replica count. `1` (default) = single instance, `3` = HA cluster. Sets `tier` accordingly. Default `1`.
- `engine_version` - (Forces new resource) MariaDB major version (e.g. `"10"`, `"11"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the instance.
- `status` - Current status. One of: `provisioning`, `active`, `error`.
- `tier` - Derived from `replicas` — `dev` (1 replica) or `prod` (3 replicas, HA).
- `endpoint_vnet_ip` - Private IP within the VNet for MySQL connections.
- `endpoint_port` - TCP port for MySQL connections (typically `3306`).
- `admin_username` - Default administrator username.
- `admin_database` - Name of the default database created at provisioning.
- `cpu_millicores` - CPU allocation in millicores. Derived from `plan`.
- `memory_mb` - Memory allocation in MB. Derived from `plan`.

## Import

MySQL instances can be imported using their UUID:

```
terraform import ccp_db_mysql_instance.app_db <instance_id>
```
