---
page_title: "ccp_db_engine_versions Data Source - cetic-cloud-platform"
subcategory: "Catalogs"
description: |-
  Lists active DB engine versions exposed to clients.
---

# ccp_db_engine_versions (Data Source)

Lists active DB engine versions (PG / MySQL / Valkey / FerretDB) exposed to clients via the CETIC Cloud API. Useful to avoid hardcoding `engine_version` strings in your modules.

## Example Usage

```hcl
data "ccp_db_engine_versions" "ferretdb" {
  engine = "ferretdb"
}

locals {
  default_ferretdb_version = one(
    [for v in data.ccp_db_engine_versions.ferretdb.versions : v if v.is_default],
  ).version
}

resource "ccp_db_ferretdb_instance" "app" {
  name           = "app-mongo"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "medium"
  tier           = "prod"
  engine_version = local.default_ferretdb_version
}
```

## Argument Reference

- `engine` - (Optional) Engine filter: `pg`, `mysql`, `valkey`, `ferretdb`. If omitted, returns versions for all engines.

## Attributes Reference

- `versions` - List of active engine versions.
  - `engine` - Engine the version belongs to.
  - `version` - Version string (used in `ccp_db_<engine>_instance.engine_version`).
  - `label` - Optional human-readable label.
  - `is_default` - Whether this version is the default suggestion in the console for its engine.
