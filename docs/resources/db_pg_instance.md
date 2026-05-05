---
page_title: "ccp_db_pg_instance Resource - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Manages a managed PostgreSQL instance (CloudNativePG) on CETIC Cloud Platform.
---

# ccp_db_pg_instance (Resource)

Manages a managed PostgreSQL instance powered by CloudNativePG (CNPG), running on a shared regional Kubernetes workload cluster. The instance is isolated in its own namespace with Kubernetes NetworkPolicies and ResourceQuotas. Data is stored on Ceph RBD persistent volumes.

~> **Note:** The `tier` argument is immutable — it determines the replica count at creation and cannot be changed. `dev` provisions 1 replica (no HA); `prod` provisions 3 replicas with pod anti-affinity for HA. To migrate from `dev` to `prod`, create a new instance and restore from a snapshot.

~> **Note:** Database provisioning is asynchronous. The provider polls until the instance reaches `active` status. `dev` tier: ~3 minutes, `prod` tier: ~5 minutes.

## Example Usage

```hcl
resource "ccp_db_pg_instance" "app_db" {
  name           = "app-postgres"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  tier           = "prod"
  engine_version = "16"
  tags           = ["database", "postgres", "env:prod"]
}

output "db_connection" {
  value     = "${ccp_db_pg_instance.app_db.admin_username}@${ccp_db_pg_instance.app_db.endpoint_host}:${ccp_db_pg_instance.app_db.endpoint_port}/${ccp_db_pg_instance.app_db.admin_database}"
  sensitive = false
}

output "db_password" {
  value     = ccp_db_pg_instance.app_db.admin_password
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the PostgreSQL instance.
- `region` - (Required, Forces new resource) Region where the instance is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC for internal network connectivity.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the database endpoint is accessible.
- `plan` - (Required, Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `tier` - (Required, Forces new resource) Service tier. One of: `dev` (1 replica, no HA, ~12 €/month base), `prod` (3 replicas, HA with anti-affinity, ~36 €/month base).

### Optional

- `engine_version` - (Optional, Forces new resource) PostgreSQL major version to deploy (e.g. `"15"`, `"16"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the instance.
- `status` - Current status. Possible values: `provisioning`, `active`, `error`.
- `endpoint_host` - Hostname or IP address for connecting to the database within the VNet.
- `endpoint_port` - TCP port for PostgreSQL connections (typically `5432`).
- `admin_username` - Database administrator username.
- `admin_database` - Name of the default database created at provisioning.
- `admin_password` - (Sensitive) Administrator password. Store securely; retrieve via `cetic db pg credentials` if needed.

## Import

PostgreSQL instances can be imported using their UUID:

```
terraform import ccp_db_pg_instance.app_db <instance_id>
```
