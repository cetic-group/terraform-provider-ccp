---
page_title: "ccp_object_bucket Resource - ccp"
subcategory: "Storage"
description: |-
  Manages an S3-compatible object storage bucket on CETIC Cloud Platform.
---

# ccp_object_bucket (Resource)

Manages an S3-compatible object storage bucket on CETIC Cloud Platform. Buckets are accessible via any standard S3 client or SDK using the region endpoint. Data is isolated at the tenant level. Use `ccp_object_storage_key` to create scoped access credentials for the bucket.

~> **Note:** Bucket names must be unique within a region. Once created, the bucket name and region cannot be changed — these fields force a new resource.

## Example Usage

```hcl
resource "ccp_object_bucket" "assets" {
  name   = "acme-app-assets"
  region = "RNN"
  tags   = ["assets", "public", "env:prod"]
}

resource "ccp_object_bucket" "backups" {
  name   = "acme-db-backups"
  region = "RNN"
  tags   = ["backups", "private"]
}

# Direct uploads from a web page: the browser may PUT to the bucket from
# these origins only.
resource "ccp_object_bucket" "uploads" {
  name   = "acme-user-uploads"
  region = "RNN"

  cors_rule {
    allowed_origins = ["https://app.example.com", "https://admin.example.com"]
    allowed_methods = ["PUT", "POST"]
    allowed_headers = ["Content-Type"]
    expose_headers  = ["ETag"]
    max_age_seconds = 3600
  }

  cors_rule {
    allowed_origins = ["https://app.example.com"]
    allowed_methods = ["GET", "HEAD"]
  }
}

output "assets_endpoint" {
  value = ccp_object_bucket.assets.endpoint_url
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Name of the bucket. Must be globally unique within the region and follow S3 bucket naming rules (lowercase, 3–63 characters, no underscores).
- `region` - (Required, Forces new resource) Region where the bucket is created. One of: `RNN`, `PAR`, `ABJ`.

### Optional

- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

### Blocks

- `cors_rule` - (Optional, repeatable) A CORS rule, applied in the order written. Without any `cors_rule` block, CORS is **not managed** by this resource: a configuration made from the console or the CLI is left untouched. Removing every block of a bucket that had some deletes its rules.
  - `allowed_origins` - (Required) Set of 1–20 origins, e.g. `https://app.example.com`. Write them in the form the platform stores: **lowercase scheme and host, no path, no trailing slash, no default port** (`:443` for `https`, `:80` for `http`) — otherwise validation refuses the value and gives the expected form. A single subdomain wildcard (`https://*.example.com`) is accepted only if the platform allows it. A bare `*` is only valid on a public bucket (`is_public = true`), alone in its rule and without `PUT`, `POST` or `DELETE`.
  - `allowed_methods` - (Required) Set among `GET`, `PUT`, `POST`, `DELETE`, `HEAD`. The platform may restrict this list.
  - `allowed_headers` - (Optional) Set of up to 20 request headers the browser may send, e.g. `Content-Type`. `*` is only accepted at the end of a name (`*` alone, or `x-amz-meta-*`). Two names that differ only by case are refused. Defaults to an empty set. Prefer an explicit list over `*`.
  - `expose_headers` - (Optional) Set of up to 20 response headers exposed to the page, e.g. `ETag`. Same format rules. Defaults to an empty set.
  - `max_age_seconds` - (Optional) How long the browser may cache the preflight answer, `0`–`86400`. Default `3600`.
  - `description` - (Optional) Free-form label, 1–200 characters, no leading or trailing space.

A bucket accepts up to 100 rules in the schema; the platform applies its own, usually lower, limit (20 by default) and may refuse `localhost` origins or wildcards. Limits that depend only on the configuration are reported at `terraform validate` / `plan`; those that depend on the platform settings are reported at apply time.

### Behaviour to know

- **Without any `cors_rule` block, CORS is not managed.** A configuration set from the console or the CLI — including on an imported bucket — is left untouched by any change of this resource. Removing every block of a bucket that had some deletes its rules.
- **A refusal at creation does not replace the bucket.** If the platform refuses the rules while the bucket is being created, Terraform keeps the bucket, reports a warning, and the rules stay planned: fix the block and apply again.
- **Rules are applied to the bucket when the apply returns.** If the platform saved them but could not apply them, the next refresh reports a warning with the reason; the platform retries on its own. A change made outside Terraform to a bucket that has `cors_rule` blocks appears as a difference at the next plan.
- **Concurrent changes.** Each write sends the revision recorded in the state at the last refresh (`cors_revision`), so a change made by someone else after your `plan` — including between the `plan` and the `apply` of a saved plan — is refused instead of overwritten: run `terraform plan` again to see it. A state created before this attribute existed has no revision yet; the first write then reads the current one just before writing, and records it.
- **Making a bucket private** while removing its `*` rule in the same apply works: the rules are written before the visibility.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the bucket.
- `cors_revision` - Revision of the CORS rules as of the last refresh or write; null when no `cors_rule` block is managed. Used internally to refuse a write when the rules changed elsewhere in the meantime.
- `endpoint_url` - S3 endpoint URL for this region (e.g. `https://s3-rnn.cloud.cetic-group.com`). Use this as the `endpoint_url` in your S3 client configuration.
- `access_key` - S3 access key. **Sensitive.** Null when the API key running Terraform is not allowed to read it — see the note below.
- `secret_key` - S3 secret key paired with `access_key`. **Sensitive.** Same nullability rule.

### Credentials and API key scope

`access_key` and `secret_key` are read from `GET /v1/buckets/{id}/credentials`,
which hands out the tenant's S3 **master key** — it opens every bucket of the
region, not just this one. The platform therefore denies that endpoint to
`write`-scoped API keys: `bucket:GetCredentials` sits in the
`READ_ONLY_ALL_SENSITIVE_DISCLOSURE_DENY` list, and a `write` key inherits both
`Member` and `ReadOnlyAll`, where an explicit Deny beats any Allow.

With such a key the bucket is created normally and both attributes are **null**,
with a warning explaining why. This is a permanent IAM decision, not a transient
failure: re-running `terraform apply` or attaching another role will not change
it. Read the credentials with an admin-scoped key or from the console — or
better, issue a bucket-scoped key with `ccp_bucket_key` instead of handing the
master key to your configuration.

Because the refusal is permanent, the provider stops asking: once both
attributes are null in state, refresh no longer calls the endpoint, so the
warning appears at creation and not on every subsequent `plan`. A key that
holds the permission keeps refreshing them normally — and one that later loses
it still gets the warning, since state then holds values.

## Import

Object buckets can be imported using their UUID:

```
terraform import ccp_object_bucket.assets <bucket_id>
```
