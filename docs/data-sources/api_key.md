---
page_title: "ccp_api_key Data Source - ccp"
subcategory: "Identity"
description: |-
  Look up an API key (metadata only).
---

# ccp_api_key (Data Source)

Look up an API key by `id`. Lookup by name is not supported.

~> The bearer token is NEVER exposed by this datasource — it is only returned once at creation time on the `ccp_api_key` resource.

## Attributes Reference

- `id`, `name`, `prefix`
- `scopes` — list of granted scopes
- `expires_at`, `last_used_at` (nullable, RFC 3339)
- `created_at`
