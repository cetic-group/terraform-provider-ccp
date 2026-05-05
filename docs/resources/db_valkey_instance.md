---
page_title: "ccp_db_valkey_instance Resource - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Manages a managed Valkey (Redis-compatible) instance on CETIC Cloud Platform.
---

# ccp_db_valkey_instance (Resource)

Manages a managed Valkey instance — an open-source, BSD-licensed Redis fork fully compatible with the Redis protocol and all Redis clients. Instances run on the shared regional Kubernetes workload cluster. Data is persisted on high-performance block storage.

~> **Note:** The `tier` argument is immutable — it determines the replica count at creation and cannot be changed. `dev` provisions 1 replica (no HA); `prod` provisions 3 replicas with anti-affinity for HA. To migrate from `dev` to `prod`, create a new instance.

~> **Note:** Database provisioning is asynchronous. The provider polls until the instance reaches `active` status (~3 minutes for `dev`, ~5 minutes for `prod`).

## Example Usage

```hcl
resource "ccp_db_valkey_instance" "app_cache" {
  name           = "app-cache"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.web.id
  plan           = "small"
  tier           = "prod"
  engine_version = "8"
  tags           = ["cache", "valkey", "env:prod"]
}

output "redis_url" {
  value     = "redis://:${ccp_db_valkey_instance.app_cache.admin_password}@${ccp_db_valkey_instance.app_cache.endpoint_host}:${ccp_db_valkey_instance.app_cache.endpoint_port}"
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the Valkey instance.
- `region` - (Required, Forces new resource) Region where the instance is created. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC for internal network connectivity.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the Valkey endpoint is accessible.
- `plan` - (Required, Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `tier` - (Required, Forces new resource) Service tier. One of: `dev` (1 replica, no HA), `prod` (3 replicas, HA with anti-affinity).

### Optional

- `engine_version` - (Optional, Forces new resource) Valkey major version to deploy (e.g. `"7"`, `"8"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the instance.
- `status` - Current status. Possible values: `provisioning`, `active`, `error`.
- `endpoint_host` - Hostname or IP address for connecting to Valkey within the VNet.
- `endpoint_port` - TCP port for Valkey/Redis connections (typically `6379`).
- `admin_password` - (Sensitive) Authentication password for the Valkey instance.

## Import

Valkey instances can be imported using their UUID:

```
terraform import ccp_db_valkey_instance.app_cache <instance_id>
```
