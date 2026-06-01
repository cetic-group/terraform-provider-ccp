---
page_title: "ccp_db_pg_instance Data Source - ccp"
subcategory: "Databases"
description: |-
  Look up a PostgreSQL database instance.
---

# ccp_db_pg_instance (Data Source)

Look up a PostgreSQL DBaaS instance by `id` or `name`.

~> Credentials are NOT surfaced by this datasource — use [`ccp_db_pg_credentials`](./db_pg_credentials.md).

## Example Usage

```hcl
data "ccp_db_pg_instance" "app" {
  name = "app-pg"
}
```

## Attributes Reference

- `id`, `name`, `region`, `engine`, `engine_version` (nullable), `tier`, `plan`
- `vpc_id`, `vnet_id`
- `status`, `endpoint_vnet_ip` (nullable), `endpoint_port` (nullable)
- `admin_username` (nullable), `admin_database` (nullable)
- `replicas`, `storage_gb`, `cpu_millicores`, `memory_mb`
- `error_message` (nullable)
- `tags`
- `public_ip_id`, `public_ip_address` (nullable)
