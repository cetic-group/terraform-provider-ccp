---
page_title: "ccp_db_ferretdb_instance Data Source - cetic-cloud-platform"
subcategory: "Database"
description: |-
  Look up a FerretDB (MongoDB-compatible) instance.
---

# ccp_db_ferretdb_instance (Data Source)

Look up a FerretDB DBaaS instance by `id` or `name`.

## Attributes Reference

- `id`, `name`, `region`, `engine`, `engine_version` (nullable), `tier`, `plan`
- `vpc_id`, `vnet_id`
- `status`, `endpoint_vnet_ip` (nullable), `endpoint_port` (nullable)
- `admin_username` (nullable), `admin_database` (nullable)
- `replicas`, `storage_gb`, `cpu_millicores`, `memory_mb`
- `error_message` (nullable)
- `tags`
- `public_ip_id`, `public_ip_address` (nullable)
