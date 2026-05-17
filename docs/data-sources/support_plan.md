---
page_title: "ccp_support_plan Data Source - terraform-provider-cetic-cloud-platform"
subcategory: "Support"
description: |-
  Read-only catalogue entry for a CETIC support plan.
---

# ccp_support_plan (Data Source)

Returns the catalogue details for a given support plan key — useful to
expose price / SLA / channels in modules without hardcoding.

## Example Usage

```hcl
data "ccp_support_plan" "premium" {
  key = "premium"
}

output "premium_monthly_price" {
  value = "${data.ccp_support_plan.premium.price_eur_month} €/mois"
}
```

## Argument Reference

### Required

- `key` (String) — Plan key (e.g. `base`, `standard`, `premium`).

## Attributes Reference

- `id` (String) — Plan UUID.
- `display_name` (String) — Display name shown in the console.
- `description` (String) — Marketing description.
- `price_eur_month_cents` (Int) — Monthly price in EUR cents.
- `price_eur_month` (Float) — Monthly price in EUR (convenience).
- `sla_first_response_hours` (Int) — First-response SLA in hours.
- `sla_resolution_hours` (Int) — Resolution SLA in hours (`0` for best-effort).
- `max_priority` (String) — Maximum ticket priority allowed.
- `channels` (List of String) — Supported channels (`email`, `chat`, `phone`).
- `is_default` (Bool) — Whether this is the catalogue's default plan.
- `is_active` (Bool) — Whether the plan is published.
- `features` (Map of String) — Free-form marketing bullets.
