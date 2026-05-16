---
page_title: "ccp_budget Resource - terraform-provider-cetic-cloud-platform"
subcategory: "Billing"
description: |-
  Monthly budget cap for the current tenant + email alerts at configurable thresholds + optional hard-stop.
---

# ccp_budget (Resource)

Sets a monthly budget cap for the current tenant. Email alerts are sent at each configured threshold (default 50/80/100%). Setting `hard_stop_at_100 = true` blocks any resource creation once MTD usage reaches the cap (HTTP 402 from the API). Billing remains hourly — this is a safety net, not a contract.

## Example Usage

```hcl
resource "ccp_budget" "main" {
  monthly_budget_cents = 5000           # 50 €/month
  alert_thresholds_pct = [50, 80, 100]
  notify_emails        = ["finance@example.com", "cto@example.com"]
  hard_stop_at_100     = true
}
```

## Argument Reference

- `monthly_budget_cents` (Required, Int) — Cap in EUR cents (ex: `5000` for 50€).
- `alert_thresholds_pct` (Optional, List of Int) — Percentage thresholds. Default `[50, 80, 100]`.
- `notify_emails` (Optional, List of String) — Email recipients. If empty, the tenant account email is used.
- `hard_stop_at_100` (Optional, Bool) — If true, resource creation is blocked once MTD usage reaches the cap. Default `false`.

## Attributes Reference

- `id`
- `tenant_id` — Resolved tenant ID.
- `currency` — Always `eur` for now.
- `last_alert_threshold_pct` — Most recent threshold that triggered an alert this month (or null).
- `active`

## Import

```bash
terraform import ccp_budget.main <budget_id>
```
