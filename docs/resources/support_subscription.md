---
page_title: "ccp_support_subscription Resource - terraform-provider-ccp"
subcategory: "Support"
description: |-
  Active CETIC support subscription for the current tenant.
---

# ccp_support_subscription (Resource)

Manages the current tenant's subscription to a CETIC support plan
(vague C6). Only one active subscription per tenant. Destroying the
resource downgrades to the default `base` (free) plan.

If the target plan is paid and the tenant has no payment method on file,
the API returns `402 Payment Required` and the apply fails — add a card
via the console first.

## Example Usage

```hcl
resource "ccp_support_subscription" "main" {
  plan_key = "standard"
}
```

Use a datasource to drive the choice from configuration:

```hcl
data "ccp_support_plan" "standard" {
  key = "standard"
}

resource "ccp_support_subscription" "main" {
  plan_key = data.ccp_support_plan.standard.key
}
```

## Argument Reference

### Required

- `plan_key` (String) — Plan key: `base`, `standard`, `premium`, or a custom
  key configured by a CETIC admin.

## Attributes Reference

- `id` (String) — Subscription row UUID.
- `tenant_id` (String) — Tenant UUID.
- `started_at` (String) — RFC3339 timestamp of the current subscription start.
- `reason` (String) — Reason for the last switch (`user_changed`,
  `admin_grant`, `initial`).

## Import

```bash
terraform import ccp_support_subscription.main <subscription_uuid>
```
