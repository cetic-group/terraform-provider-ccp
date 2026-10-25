---
page_title: "ccp_pricing Data Source - terraform-provider-ccp"
subcategory: "Billing"
description: |-
  Lists the live pricing catalog. Filter by resource_type, plan and replica_count.
---

# ccp_pricing (Data Source)

Reads the active pricing grid edited by the platform admin via the backoffice. Useful to drive cost estimates from HCL, gate resource creation against a budget computation, or build a portal page.

## Example Usage

```hcl
# Whole grid
data "ccp_pricing" "all" {}

output "total_rows" {
  value = length(data.ccp_pricing.all.items)
}

# Filter by type
data "ccp_pricing" "containers" {
  resource_type = "container"
}

# Filter by type + plan
data "ccp_pricing" "small_container" {
  resource_type = "container"
  plan          = "small"
}

output "monthly_eur" {
  value = data.ccp_pricing.small_container.items[0].monthly_price_eur
}
```

## Argument Reference

- `resource_type` (Optional) — Filter on resource type (ex: `container`, `vm`, `block_volume`, `db_instance`, `k8s_cluster_hcp`, `k8s_node`, `registry`, `appgw`, `appgw_cache`, `public_ip`, `load_balancer`, `vnet_peering`, `object_storage`, `snapshot`, `template`).
- `plan` (Optional) — Filter on plan (ex: `nano`, `small`, `prod:medium`). Combined with `resource_type`.
- `replica_count` (Optional) — Filter on `1` or `2` instances for AppGW, LB or the AppGW cache option.

## Attributes Reference

- `items` (List of Object) — Active pricing rows matching the filter. Each item:
  - `id` — UUID of the pricing row.
  - `resource_type`
  - `plan` — Plan key or null for flat-priced resources.
  - `replica_count` — Number of instances for this tariff, or `null` when not applicable or absent from an older platform.
  - `hourly_price_cents` — Price in cents per billing unit, including fractional cents. See `billing_dimension` for the unit.
  - `monthly_price_eur` — Monthly estimate returned by the platform; `null` for pricing per consumed unit.
  - `yearly_price_eur` — Yearly estimate returned by the platform; `null` for pricing per consumed unit.
  - `currency` — Usually `eur`.
  - `description`
  - `is_free` — True if marked free (collector skips billing).
  - `billing_dimension` — `flat_hourly` / `per_gb_hourly` / `per_gb_egress` / `per_million_requests`.
  - `stopped_disk_price_cents_per_gb_hour` — When set, stopped instances of this type are billed at this rate per GB of disk per hour.
  - `monthly_commit_discount_pct` — Discount applied when a monthly commit is active.
  - `yearly_commit_discount_pct` — Discount applied when a yearly commit is active.

A `null` periodic estimate is unavailable; it does not mean the resource is
free. Explicit zero amounts remain zero. Guard nullable estimates in HCL
before arithmetic; the provider preserves the platform values without
recalculating prices.

## Gateway tariff variants

The catalog defines independent base rates for one and two instances. Each
amount already covers the selected mode: do not multiply it by `replica_count`.
The `appgw_cache` resource type contains the additional charge for the optional
Application Gateway HTTP cache, also priced for the selected mode. Enabling
`cache_enabled` opts into that charge; disabling it removes the option.
The cache dimensions remain determined by the capacity plan.

```hcl
data "ccp_pricing" "gateway_single" {
  resource_type = "appgw"
  plan          = "appgw-small"
  replica_count = 1
}

data "ccp_pricing" "cache_single" {
  resource_type = "appgw_cache"
  plan          = "appgw-small"
  replica_count = 1
}
```

Use the exact plan key returned by the pricing catalog. Missing variants
produce no matching items; they never fall back to another mode's price.
For compatibility, historical AppGW/LB base rows without instance-count
metadata match a filter of `2`, since those platforms only supported high
availability. Their output metadata remains `null`; the rule never invents a
single-instance or cache rate. For a combined quote, use the platform's price
estimator, for example `cetic billing estimate appgw --plan small
--replica-count 1 --cache-enabled`.
