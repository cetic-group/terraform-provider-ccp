---
page_title: "ccp_object_bucket Data Source - cetic-cloud-platform"
subcategory: "Storage"
description: |-
  Look up an S3 Object Bucket by ID or (name, region).
---

# ccp_object_bucket (Data Source)

Look up an S3 (Ceph RGW) Object Bucket. Credentials are not surfaced.

## Example Usage

```hcl
data "ccp_object_bucket" "uploads" {
  name   = "uploads"
  region = "RNN"
}
```

## Attributes Reference

- `id`, `name`, `region`, `endpoint_url` (nullable)
- `size_bytes`, `status`, `is_public`
- `error_message` (nullable)
- `tags`, `created_at`, `updated_at`
