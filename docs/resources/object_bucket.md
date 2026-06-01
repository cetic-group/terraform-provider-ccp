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

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the bucket.
- `endpoint_url` - S3 endpoint URL for this region (e.g. `https://s3-rnn.cloud.cetic-group.com`). Use this as the `endpoint_url` in your S3 client configuration.

## Import

Object buckets can be imported using their UUID:

```
terraform import ccp_object_bucket.assets <bucket_id>
```
