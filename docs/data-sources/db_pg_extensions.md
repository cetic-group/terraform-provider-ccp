---
page_title: "ccp_db_pg_extensions Data Source - ccp"
subcategory: "Databases"
description: |-
  Extension catalog of a PostgreSQL major version.
---

# ccp_db_pg_extensions (Data Source)

Extension catalog of a PostgreSQL major version: the extensions that can be listed in `allowed_extensions` of a [`ccp_db_pg_instance`](../resources/db_pg_instance.md#extensions), the libraries that can be preloaded, and the extensions the database owner can create directly, without authorization.

## Example Usage

```hcl
data "ccp_db_pg_extensions" "pg16" {
  version = 16
}

output "allowable" {
  value = [for e in data.ccp_db_pg_extensions.pg16.extensions : e.name]
}

output "create_directly" {
  value = data.ccp_db_pg_extensions.pg16.direct_extensions
}
```

## Argument Reference

- `version` - (Optional) PostgreSQL major version. Defaults to `16`. A version without a catalog fails the read.

## Attributes Reference

- `extensions` - Extensions that can be listed in `allowed_extensions`:
  - `name` - Name, as used in `CREATE EXTENSION`.
  - `description` - Short description.
  - `requires_preload` - The extension must also be listed in `preload_libraries`.
  - `requires` - Extensions it depends on, to list in `allowed_extensions` as well.
  - `single_database` - The extension works in a single database of the instance (see `cron_database`).
- `preload_libraries` - Libraries that can be listed in `preload_libraries`.
- `direct_extensions` - Extensions the database owner can create directly with `CREATE EXTENSION`, without listing them in `allowed_extensions`.
