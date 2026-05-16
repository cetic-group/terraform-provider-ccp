---
page_title: "ccp_pricing Data Source - terraform-provider-cetic-cloud-platform"
subcategory: "Billing"
description: |-
  Lists the live pricing grid (resource_pricing). Filter by resource_type and/or plan.
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

- `resource_type` (Optional) — Filter on resource type (ex: `container`, `vm`, `block_volume`, `db_instance`, `k8s_cluster_hcp`, `k8s_node`, `registry`, `appgw`, `public_ip`, `load_balancer`, `vnet_peering`, `object_storage`, `snapshot`, `template`).
- `plan` (Optional) — Filter on plan (ex: `nano`, `small`, `prod:medium`). Combined with `resource_type`.

## Attributes Reference

- `items` (List of Object) — Active pricing rows matching the filter. Each item:
  - `id` — UUID of the pricing row.
  - `resource_type`
  - `plan` — Plan key or null for flat-priced resources.
  - `hourly_price_cents` — Price in cents per hour.
  - `monthly_price_eur` — `hourly_price_cents × 730 / 100`.
  - `yearly_price_eur` — `hourly_price_cents × 8760 / 100`.
  - `currency` — Usually `eur`.
  - `description`
  - `is_free` — True if marked free (collector skips billing).
  - `billing_dimension` — `flat_hourly` / `per_gb_hourly` / `per_gb_egress` / `per_million_requests`.
  - `stopped_disk_price_cents_per_gb_hour` — When set, stopped instances of this type are billed at this rate per GB of disk per hour.
  - `monthly_commit_discount_pct` — Discount applied when a monthly commit is active.
  - `yearly_commit_discount_pct` — Discount applied when a yearly commit is active.
