---
page_title: "ccp_service_account Resource - ccp"
subcategory: "Identity"
description: |-
  Manages a CETIC Cloud service account — a token-based machine identity that derives permissions from IAM role assignments.
---

# ccp_service_account (Resource)

Manages a CETIC Cloud service account. Service accounts are machine
identities that authenticate via a Bearer token with the `ccp_sa_` prefix
(distinct from API keys' `ccp_live_` prefix). Unlike API keys, service
accounts have **no static `scope`** — their permissions come entirely from
IAM role assignments (`ccp_iam_role_assignment` with
`principal_type = "service_account"`).

~> **`token` is returned only at creation** and never re-emitted by the
API. It is written to the Terraform state and is `Sensitive`. Treat your
state backend as a secret store.

~> **To rotate the token, taint the resource.** The destroy + create cycle
issues a fresh token. The API exposes an inline rotation endpoint
(`POST /v1/service-accounts/{id}/rotate`), but Terraform's declarative
model maps poorly to it — taint or `terraform apply -replace` are the
idiomatic answers.

~> **`name` / `description` are mutable in place.** Terraform issues a
`PATCH` rather than a replace when these change. `expires_at` is not
mutable in place — change it by recreating the resource.

## Example Usage

```hcl
resource "ccp_service_account" "ci" {
  name        = "ci-pipeline"
  description = "GitHub Actions runner for the platform repo"
  expires_at  = "2027-05-10T00:00:00Z"
}

# Attach IAM permissions
resource "ccp_iam_role" "ci_pusher" {
  name                 = "RegistryPusher"
  policy_document_json = data.ccp_iam_policy_document.registry_push.json
}

resource "ccp_iam_role_assignment" "ci_can_push" {
  role_id        = ccp_iam_role.ci_pusher.id
  principal_type = "service_account"
  principal_id   = ccp_service_account.ci.id
}

# Surface the token to your secret store
output "ci_sa_token" {
  value     = ccp_service_account.ci.token
  sensitive = true
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name (1-64 chars). Must be unique
  within the tenant.

### Optional

- `description` - (Optional) Free-form description (max 512 chars).
- `expires_at` - (Optional) RFC 3339 timestamp after which the token is
  rejected by the API. Omit for no expiry.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the service account.
- `token` - (Sensitive) Full token (`ccp_sa_<43 chars>`). Returned **only
  at creation**.
- `token_prefix` - Visible token prefix (e.g. `ccp_sa_xxxxxxxx`), safe to
  log for identification.
- `last_used_at` - RFC 3339 timestamp of the most recent request
  authenticated with this token.
- `rotated_at` - RFC 3339 timestamp of the last server-side rotation
  (always null for SAs managed via Terraform).
- `created_at` - RFC 3339 creation timestamp.
- `updated_at` - RFC 3339 timestamp of the last update.

## Import

Service accounts are imported using their UUID:

```
terraform import ccp_service_account.ci <service_account_id>
```

~> **Note:** After import, `token` is `null` in state — the API does not
re-emit it. Taint the resource to issue a fresh token.
