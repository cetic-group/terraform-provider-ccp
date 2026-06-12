# End-to-end example for IAM Roles v1 — composes a custom registry-admin
# role from an ergonomic policy document, provisions a service account
# for CI/CD, attaches the role to it, and looks up the built-in
# BillingReader role for a separate API-key principal.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 4.8"
    }
  }
}

provider "ccp" {}

# ─── Inputs ──────────────────────────────────────────────────────────────────

variable "tenant_id" {
  type        = string
  description = "UUID of the calling tenant (used to scope ARNs in custom policies). Find it via `cetic auth whoami` or the console URL."
}

variable "region" {
  type        = string
  description = "Region code for the registry ARNs scoped by the custom role (`rnn`, `par`, `abj`)."
  default     = "rnn"
}

# ─── 1. Compose the policy document for a registry administrator ─────────────

data "ccp_iam_policy_document" "registry_admin" {
  statement {
    sid       = "AllowRegistryFullControl"
    effect    = "Allow"
    actions   = ["registry:*"]
    resources = ["arn:ccp:registry:${var.region}:${var.tenant_id}:registry/*"]
  }

  # Belt-and-braces guard: even a misconfigured user with this role cannot
  # delete a registry. (Deny wins over Allow during evaluation.)
  statement {
    sid       = "DenyRegistryDeletion"
    effect    = "Deny"
    actions   = ["registry:DeleteRegistry"]
    resources = ["*"]
  }
}

# ─── 2. Create the custom role from that policy ─────────────────────────────

resource "ccp_iam_role" "registry_admin" {
  name                 = "RegistryAdminNoDelete"
  description          = "Manage all registries in ${var.region}, except deletion."
  policy_document_json = data.ccp_iam_policy_document.registry_admin.json
}

# ─── 3. Service account for CI/CD that will own this role ────────────────────

resource "ccp_service_account" "ci" {
  name        = "ci-pipeline"
  description = "GitHub Actions runner for the platform repo"
  expires_at  = "2027-05-10T00:00:00Z"
}

# Surface the SA token to the outside world (sensitive, one-shot).
output "ci_sa_token" {
  value       = ccp_service_account.ci.token
  sensitive   = true
  description = "Service-account token (`ccp_sa_*`) to plug into the CI runner's secrets."
}

# ─── 4. Attach the role to the service account ───────────────────────────────

resource "ccp_iam_role_assignment" "ci_can_push" {
  role_id        = ccp_iam_role.registry_admin.id
  principal_type = "service_account"
  principal_id   = ccp_service_account.ci.id
  expires_at     = "2027-05-10T00:00:00Z"
}

# ─── 5. Look up the built-in BillingReader role for a separate principal ─────

data "ccp_iam_role" "billing_reader" {
  name     = "BillingReader"
  built_in = true
}

resource "ccp_api_key" "finance" {
  name   = "finance-dashboard"
  scopes = ["read", "billing"]
}

resource "ccp_iam_role_assignment" "finance_billing" {
  role_id        = data.ccp_iam_role.billing_reader.id
  principal_type = "api_key"
  principal_id   = ccp_api_key.finance.id
}

# ─── Outputs ─────────────────────────────────────────────────────────────────

output "registry_role_id" {
  value       = ccp_iam_role.registry_admin.id
  description = "UUID of the custom role — useful for further assignments."
}

output "registry_policy_hash" {
  value       = ccp_iam_role.registry_admin.policy_hash
  description = "SHA-256 of the canonical policy — compare to detect drift."
}

output "billing_reader_built_in_id" {
  value       = data.ccp_iam_role.billing_reader.id
  description = "UUID of the platform `BillingReader` built-in role."
}
