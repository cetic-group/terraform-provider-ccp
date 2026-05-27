---
page_title: "ccp_service_account Data Source - cetic-cloud-platform"
subcategory: "Identity"
description: |-
  Look up a service account.
---

# ccp_service_account (Data Source)

Look up a service account by `id` or `name`.

~> The bearer token is NEVER exposed by this datasource — it is only returned at creation time on the `ccp_service_account` resource.

## Example Usage

```hcl
data "ccp_service_account" "ci" {
  name = "ci"
}
```

## Attributes Reference

- `id`, `tenant_id`, `org_id`, `name`, `description` (nullable)
- `token_prefix` — Public prefix of the bearer token (safe to expose).
- `last_used_at`, `expires_at`, `rotated_at` (nullable RFC 3339)
- `created_at`
