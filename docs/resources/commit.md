---
page_title: "ccp_commit Resource - terraform-provider-cetic-cloud-platform"
subcategory: "Billing"
description: |-
  Commitment-based discount on the tenant's overall consumption (-10% monthly or -20% yearly).
---

# ccp_commit (Resource)

Subscribe to a commitment that grants an immediate discount on every billable resource of the tenant. Cancelable any time — the discount remains active until `end_at`, then stops auto-renewing.

## Example Usage

```hcl
# Monthly commit — -10%
resource "ccp_commit" "monthly" {
  commit_type = "monthly"
  auto_renew  = true
}

# Yearly commit — -20%, best price
resource "ccp_commit" "yearly" {
  commit_type = "yearly"
}
```

## Argument Reference

- `commit_type` (Required, String) — `monthly` (-10% over 30 days) or `yearly` (-20% over 365 days). **Immutable** — changing requires a new resource.
- `auto_renew` (Optional, Bool) — Renew automatically at `end_at`. Default `true`.

## Attributes Reference

- `id`
- `tenant_id`
- `discount_pct` — Resolved discount (10 or 20).
- `start_at` (RFC3339)
- `end_at` (RFC3339)
- `canceled_at` (RFC3339 or empty) — Set when the commit was cancelled; the discount stays valid until `end_at`.

## Behaviour

- `terraform destroy` calls the platform `cancel` endpoint — it does not retroactively remove the discount. The tenant keeps the discount until `end_at`.
- Stacks with active promo codes (cumulative percentages, capped at 80%).

## Import

```bash
terraform import ccp_commit.yearly <commit_id>
```
