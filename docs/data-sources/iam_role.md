---
page_title: "ccp_iam_role Data Source - ccp"
subcategory: "Identity"
description: |-
  Look up a CETIC Cloud IAM role by ID, or by name (with optional built-in filter).
---

# ccp_iam_role (Data Source)

Look up an existing IAM role — either by `id` or by `name`. When looking
up by name, narrow the search with `built_in = true|false` to disambiguate
between a platform-managed built-in role and a custom tenant role of the
same name.

The 10 platform built-in roles — `AdminAll`, `ReadOnlyAll`, `Member`,
`RegistryAdmin`, `RegistryReader`, `BucketReader`, `BucketWriter`,
`K8sViewer`, `BillingReader`, `NetworkAdmin` — are stable across releases
and can be safely identified by `(name, built_in = true)`.

## Example Usage

### Lookup by UUID

```hcl
data "ccp_iam_role" "registry_admin" {
  id = "11111111-2222-3333-4444-555555555555"
}
```

### Lookup a built-in role by name

```hcl
data "ccp_iam_role" "billing_reader" {
  name     = "BillingReader"
  built_in = true
}

resource "ccp_iam_role_assignment" "finance_can_see_invoices" {
  role_id        = data.ccp_iam_role.billing_reader.id
  principal_type = "api_key"
  principal_id   = ccp_api_key.finance.id
}
```

### Lookup a custom tenant role by name

```hcl
data "ccp_iam_role" "custom_pusher" {
  name     = "RegistryPusherCustom"
  built_in = false
}
```

## Argument Reference

Provide **either** `id`, **or** `name` (optionally with `built_in`).
Setting both `id` and `name` yields an error.

### Optional

- `id` - UUID of the role to look up. Conflicts with `name`.
- `name` - Name of the role. Conflicts with `id`.
- `built_in` - When looking up by `name`, restrict to built-in roles
  (`true`) or custom roles (`false`). Leave unset to match either.

## Attributes Reference

- `id` - UUID of the role.
- `name` - Human-readable name.
- `description` - Free-form description.
- `policy_document_json` - JCS-canonicalised PolicyDocument as a JSON
  string. Can be fed directly into `ccp_iam_role.policy_document_json`
  for a custom role that mirrors a built-in's policy.
- `policy_hash` - SHA-256 hex of the canonical PolicyDocument.
- `is_built_in` - Whether this role is platform-managed (built-in) or
  tenant-managed (custom).
- `created_at` - RFC 3339 creation timestamp.
