---
page_title: "ccp_secret Data Source - cetic-cloud-platform"
subcategory: "Secret Manager"
description: |-
  Look up CETIC Cloud secret metadata by id or by name. Never returns plaintext data.
---

# ccp_secret (Data Source)

Look up an existing secret — either by `id` or by `name`. Provide exactly
one of the two.

~> **Returns metadata only.** The plaintext `data` field is **never**
exposed by a data source on this platform. To consume plaintext from
Terraform, manage the secret with the `ccp_secret` resource (which keeps
the latest plaintext in `Sensitive` state). To consume it outside
Terraform, use the `cetic secret value <id>` CLI command (audit-logged).

## Example Usage

### Lookup by UUID

```hcl
data "ccp_secret" "db_creds" {
  id = "11111111-2222-3333-4444-555555555555"
}
```

### Lookup by name (typical pattern: discover a secret managed by another team)

```hcl
data "ccp_secret" "shared_tls" {
  name = "wildcard-cetic-tls"
}

output "shared_tls_version" {
  value = data.ccp_secret.shared_tls.version
}
```

## Argument Reference

Provide **either** `id` **or** `name`. Setting both yields an error;
setting neither also yields an error.

### Optional

- `id` - UUID of the secret. Conflicts with `name`.
- `name` - DNS-friendly secret name. Conflicts with `id`.

## Attributes Reference

- `id` - UUID of the secret.
- `name` - DNS-friendly secret name.
- `description` - Free-form description (may be `null`).
- `version` - Server-side monotonic version counter, bumped on every
  rotation.
- `tags` - Free-form list of tags attached to the secret.
- `last_rotated_at` - RFC 3339 timestamp of the most recent rotation,
  or `null` if never rotated.
- `created_at` - RFC 3339 creation timestamp.
- `updated_at` - RFC 3339 timestamp of the last metadata edit or
  rotation.
