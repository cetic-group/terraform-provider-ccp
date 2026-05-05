---
page_title: "ccp_api_key Resource - cetic-cloud-platform"
subcategory: "Identity"
description: |-
  Manages a machine API key (cl_live_) on CETIC Cloud Platform.
---

# ccp_api_key (Resource)

Manages a machine API key for programmatic access to CETIC Cloud. API keys use the `cl_live_` prefix and support fine-grained scoped permissions. They are suitable for CI/CD pipelines, Terraform configurations, automation scripts, and the `cetic` CLI.

~> **Important:** The `token` attribute is returned **only at creation time** and cannot be retrieved afterwards. The full token value is written to the Terraform state — treat your state file as sensitive. Store the token in a secure location (e.g. a secrets manager or Terraform Cloud workspace variable). If the token is lost, destroy this resource and create a new key.

~> **Note:** This resource cannot be imported because the token cannot be retrieved from the API after creation.

## Example Usage

```hcl
# Read-write key for a CI/CD pipeline (expires in 1 year)
resource "ccp_api_key" "ci_pipeline" {
  name            = "gitlab-ci"
  scopes          = ["read", "write"]
  expires_in_days = 365
}

# Admin key for Terraform automation
resource "ccp_api_key" "tf_automation" {
  name   = "terraform-automation"
  scopes = ["admin"]
}

# Store the token securely as an output
output "ci_api_key_token" {
  value     = ccp_api_key.ci_pipeline.token
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable label for the API key.
- `scopes` - (Required) List of permission scopes. One or more of: `read`, `write`, `billing`, `admin`. Note: `write` implies `read`; `admin` implies all scopes.

### Optional

- `expires_in_days` - (Optional, Forces new resource) Number of days until the key expires. Valid range: 1–3650. If omitted, the key does not expire.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the API key.
- `token` - (Sensitive) The full API key token in `cl_live_...` format. Available only at creation time. The Terraform state stores this value — keep your state backend secure.
- `access_key_prefix` - First 16 characters of the token for identification (e.g. `cl_live_xxxxxxxx`). Safe to log or display.
- `expires_at` - Expiry timestamp (RFC3339) if `expires_in_days` was set, otherwise empty.
- `last_used_at` - Timestamp (RFC3339) of the most recent API request authenticated with this key.

## Import

Import is not supported for this resource. The token cannot be retrieved after creation.
