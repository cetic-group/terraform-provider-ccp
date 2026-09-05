---
page_title: "ccp_bucket_key Resource - terraform-provider-ccp"
subcategory: "Object Storage"
description: |-
  S3 access key scoped to a single bucket.
---

# ccp_bucket_key (Resource)

Creates an S3 access key **scoped to one bucket**. The credentials open that
bucket and nothing else, at the access level you pick.

This replaces `ccp_object_storage_key`. Since IAM S3 v2 (2026-05-09) the
platform no longer issues tenant-wide keys, and that resource's creation
endpoint returns `410 Gone`. It still reads, updates and destroys keys created
before that date.

~> **The secret is returned once, at creation.** The platform does expose a
reveal endpoint, but it is **single-use**: it marks the key as revealed and
every later call returns `410 Gone`. This provider therefore never calls it —
not on refresh, not on import. Keep the value in your state (or copy it into a
secret store at creation); if it is lost, revoke the key and create another.

## Example Usage

```hcl
resource "ccp_object_bucket" "backup" {
  name   = "ocohi-backup"
  region = "RNN"
}

resource "ccp_bucket_key" "backup_writer" {
  bucket_id       = ccp_object_bucket.backup.id
  label           = "ocohi-backup-writer"
  access_level    = "readwrite"
  expires_in_days = 365
}

output "s3_endpoint" {
  value = ccp_bucket_key.backup_writer.endpoint_url
}

# The name S3 expects — not the displayed one.
output "s3_bucket" {
  value = ccp_bucket_key.backup_writer.s3_bucket_name
}
```

## Argument Reference

- `bucket_id` - (Required, Forces new resource) UUID of the bucket the key is scoped to.
- `label` - (Required, Forces new resource) Human-readable label, 1–100 characters. The API has no rename route, so changing it replaces the key.
- `access_level` - (Optional) `read`, `write`, `readwrite` (default) or `full`. Changed **in place** — the platform exposes a PATCH for this field alone, so tightening or widening a key does not rotate its secret.
- `expires_in_days` - (Optional, Forces new resource) Lifetime in days, 1–3650. Omit for a key that never expires. The API accepts it only at creation and never echoes it back — read `expires_at` for the resulting date.

## Attributes Reference

- `id` - UUID of the key.
- `region` - Region of the bucket.
- `access_key` - S3 access key. **Sensitive.**
- `secret_key` - S3 secret key. **Sensitive**, and only ever populated by the creation that produced it — see the note above.
- `endpoint_url` - S3 endpoint for the region.
- `s3_bucket_name` - Bucket name **as S3 sees it**. It differs from the displayed name, and it is the one external tools expect (Terraform S3 backend, `aws` CLI, `boto3`).
- `access_key_prefix` - First characters of the access key, safe to display.
- `created_at` - RFC 3339 creation timestamp.
- `expires_at` - RFC 3339 expiry, `null` when the key never expires.
- `last_used_at` - RFC 3339 timestamp of the last use, `null` if never used.

## Destroying a key

Destroying **revokes** the key: the platform keeps the record, with a revocation
date, as an audit trail. A key revoked outside Terraform is detected on the next
refresh and removed from state, so the next plan recreates it.

## Import

A key is scoped to its bucket, so the bucket id is part of the address:

```shell
terraform import ccp_bucket_key.backup_writer <bucket_id>/<key_id>
```

~> An imported key leaves `secret_key` **null**, and no refresh will fill it:
the reveal endpoint is single-use. Import is for managing a key's lifecycle, not
for recovering its secret.
