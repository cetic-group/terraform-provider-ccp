---
page_title: "ccp_db_valkey_instance Resource - ccp"
subcategory: "Databases"
description: |-
  Manages a managed Valkey (Redis-compatible) instance on CETIC Cloud Platform.
---

# ccp_db_valkey_instance (Resource)

Manages a managed Valkey instance — an open-source, BSD-licensed Redis fork fully compatible with the Redis protocol and all Redis clients. Instances run on the shared regional Kubernetes workload cluster, isolated per tenant. Data is persisted on high-performance block storage.

~> **Note:** `replicas` is set at creation and immutable — `1` provisions a single instance (`tier = "dev"`), `3` provisions an HA cluster (`tier = "prod"`). To migrate from dev to prod, create a new instance.

~> **Note:** Provisioning is asynchronous. The provider polls until the instance reaches `active` status (~3 minutes single, ~5 minutes HA).

## Example Usage

```hcl
resource "ccp_db_valkey_instance" "app_cache" {
  name           = "app-cache"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.web.id
  plan           = "small"
  storage_gb     = 10
  replicas       = 3 # HA cluster — `tier` will be computed as "prod"
  engine_version = "8"
  tags           = ["cache", "valkey", "env:prod"]
}

output "valkey_endpoint" {
  value = "redis://${ccp_db_valkey_instance.app_cache.endpoint_vnet_ip}:${ccp_db_valkey_instance.app_cache.endpoint_port}"
}
```

Retrieve the auth password through the dedicated CLI / API endpoint (not exposed as a Terraform attribute):

```bash
cetic db valkey credentials <instance_id>
```

## Argument Reference

### Required

- `name` - Name of the Valkey instance.
- `region` - (Forces new resource) Region. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Forces new resource) UUID of the VPC.
- `vnet_id` - (Forces new resource) UUID of the VNet where the endpoint is accessible.
- `plan` - (Forces new resource) Instance plan controlling CPU and memory. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `storage_gb` - Persistent storage size in GB (for AOF / RDB persistence).

### Optional

- `replicas` - (Forces new resource) Replica count. `1` (default) = single instance, `3` = HA cluster. Sets `tier` accordingly. Default `1`.
- `engine_version` - (Forces new resource) Valkey major version (e.g. `"7"`, `"8"`). Available versions are managed by the CETIC Cloud team. Defaults to the latest available version.
- `tags` - List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the instance.
- `status` - Current status. One of: `provisioning`, `active`, `error`.
- `tier` - Derived from `replicas` — `dev` (1 replica) or `prod` (3 replicas, HA).
- `endpoint_vnet_ip` - Private IP within the VNet for Valkey/Redis connections.
- `endpoint_port` - TCP port (typically `6379`).
- `cpu_millicores` - CPU allocation in millicores. Derived from `plan`.
- `memory_mb` - Memory allocation in MB. Derived from `plan`.

## Import

Valkey instances can be imported using their UUID:

```
terraform import ccp_db_valkey_instance.app_cache <instance_id>
```
