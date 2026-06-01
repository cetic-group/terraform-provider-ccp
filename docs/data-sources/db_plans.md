---
page_title: "ccp_db_plans Data Source - ccp"
subcategory: "Catalogs"
description: |-
  Lists database plans (CPU/memory tiers) available for managed DB instances.
---

# ccp_db_plans (Data Source)

Lists database plans (CPU/memory tiers + indicative price) for managed DB instances. Filter by `engine` to get only plans for a specific backend.

## Example Usage

```hcl
# All plans for PostgreSQL
data "ccp_db_plans" "pg" {
  engine = "pg"
}

# Use the default PG plan in a resource
locals {
  default_pg_plan = one([for p in data.ccp_db_plans.pg.plans : p if p.is_default])
}

resource "ccp_db_pg_instance" "main" {
  name           = "app-pg"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = local.default_pg_plan.key
  tier           = "prod"
  engine_version = "16"
}
```

## Argument Reference

- `engine` - (Optional) Engine filter: `pg`, `mysql`, `valkey`, `ferretdb`. If omitted, returns plans for all engines.

## Attributes Reference

- `plans` - List of active DB plans.
  - `key` - Plan key (used in `ccp_db_<engine>_instance.plan`).
  - `name` - Human-readable plan name (may be null).
  - `engine` - Engine the plan belongs to.
  - `cpu_millicores` - CPU request/limit, in millicores.
  - `memory_mb` - Memory request/limit, in mebibytes.
  - `price_eur_month` - Indicative monthly price in EUR (null if billing not configured for the plan).
  - `is_default` - Whether this plan is the default suggestion in the console.
