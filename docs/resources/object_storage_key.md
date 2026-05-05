---
page_title: "ccp_object_storage_key Resource - cetic-cloud-platform"
subcategory: "Storage"
description: |-
  Manages a scoped S3 access key for object storage on CETIC Cloud Platform.
---

# ccp_object_storage_key (Resource)

Manages a scoped S3 access key (subuser) for object storage. Keys are scoped to a region and an access level, and are compatible with any S3 client or SDK. The `access_key` and `secret_key` credentials are returned **only at creation time** — the secret key cannot be retrieved afterwards.

~> **Important:** Store the `secret_key` output securely immediately after `terraform apply` (for example, in a secrets manager or Terraform Cloud variable). It cannot be recovered from the API after creation. If lost, destroy this resource and create a new key.

~> **Note:** This resource cannot be imported because the credentials are not recoverable from the API.

## Example Usage

```hcl
resource "ccp_object_storage_key" "ci_pipeline" {
  region          = "RNN"
  label           = "ci-pipeline-readonly"
  access_level    = "read"
  expires_in_days = 365
}

resource "ccp_object_storage_key" "app_uploader" {
  region       = "RNN"
  label        = "app-asset-uploader"
  access_level = "readwrite"
}

# Expose credentials as sensitive outputs
output "ci_access_key" {
  value     = ccp_object_storage_key.ci_pipeline.access_key
  sensitive = true
}

output "ci_secret_key" {
  value     = ccp_object_storage_key.ci_pipeline.secret_key
  sensitive = true
}

output "s3_endpoint" {
  value = ccp_object_storage_key.ci_pipeline.endpoint_url
}
```

## Argument Reference

### Required

- `region` - (Required, Forces new resource) Region where the key is valid. One of: `RNN`, `PAR`, `ABJ`.
- `label` - (Required) Human-readable label for identifying the key in the console.
- `access_level` - (Required, Forces new resource) Permission level. One of: `read` (GET/HEAD only), `write` (PUT/DELETE only), `readwrite` (GET + PUT + DELETE), `full` (all operations including bucket management).

### Optional

- `expires_in_days` - (Optional, Forces new resource) Number of days until the key expires. Valid range: 1–3650. If omitted, the key does not expire.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the access key.
- `access_key` - (Sensitive) S3 access key ID. Available only at creation time. Treat the Terraform state file as sensitive.
- `secret_key` - (Sensitive) S3 secret access key. Available only at creation time. Treat the Terraform state file as sensitive.
- `endpoint_url` - S3 endpoint URL for this region (e.g. `https://s3-rnn.cloud.cetic-group.com`).
- `access_key_prefix` - First 8 characters of the access key for identification in the console.

## Import

Import is not supported for this resource. Credentials cannot be retrieved after creation.
