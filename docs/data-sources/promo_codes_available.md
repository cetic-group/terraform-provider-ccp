---
page_title: "ccp_promo_codes_available Data Source - terraform-provider-cetic-cloud-platform"
subcategory: "Billing"
description: |-
  Lists publicly-available promo codes the current tenant can apply.
---

# ccp_promo_codes_available (Data Source)

Lists publicly-available promo codes. Useful to display in a portal or to programmatically discover codes such as `LAUNCH2026`.

## Example Usage

```hcl
data "ccp_promo_codes_available" "all" {}

output "active_codes" {
  value = [for c in data.ccp_promo_codes_available.all.codes : "${c.code} (-${c.discount_pct}% × ${c.duration_months}mo)"]
}
```

## Attributes Reference

- `codes` (List of Object) — Each code:
  - `id`
  - `code` — Uppercase (ex: `LAUNCH2026`).
  - `description`
  - `discount_pct` — 1-100.
  - `duration_months` — How many months the discount stays active for the tenant after apply.
