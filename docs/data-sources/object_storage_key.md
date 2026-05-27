---
page_title: "ccp_object_storage_key Data Source - cetic-cloud-platform"
subcategory: "Storage"
description: |-
  Look up an Object Storage subuser key (metadata only).
---

# ccp_object_storage_key (Data Source)

Look up an Object Storage subuser key (RGW) by `id`. Lookup by label is not supported.

~> The secret access key is NEVER exposed — only returned once at creation time on the `ccp_object_storage_key` resource.

## Attributes Reference

- `id`, `region`, `label`, `access_level`, `access_key_prefix`
- `created_at`, `expires_at` (nullable), `revoked_at` (nullable)
