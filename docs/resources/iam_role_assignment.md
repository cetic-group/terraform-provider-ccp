---
page_title: "ccp_iam_role_assignment Resource - cetic-cloud-platform"
subcategory: "Identity"
description: |-
  Attaches a CETIC Cloud IAM role to a principal (org member, API key, service account, CCKS workload).
---

# ccp_iam_role_assignment (Resource)

Attaches an IAM role to a principal. The 4 supported `principal_type`s are:

- `org_member` — a human collaborator invited via `ccp_org_member`.
- `api_key` — a machine API key (`ccp_live_*`).
- `service_account` — a token-based service account (`ccp_sa_*`).
- `ccks_workload` — a Kubernetes workload identity exchanged through
  `POST /v1/auth/ccks-exchange`.

~> **All attributes force replacement.** v1 keeps assignments immutable.
Changing the `role_id`, `principal_type`, `principal_id` or `expires_at`
destroys and re-creates the assignment.

~> **Expiry is enforced server-side.** Past `expires_at`, the assignment
is ignored during policy evaluation — but the row stays in the database
for audit. Run a periodic `terraform apply -refresh-only` or a cleanup
job to reap expired assignments from state.

## Example Usage

### Attach to a service account

```hcl
resource "ccp_service_account" "ci" {
  name        = "ci-pipeline"
  description = "GitHub Actions runner"
}

resource "ccp_iam_role" "registry_admin" {
  name                 = "RegistryAdminCustom"
  policy_document_json = data.ccp_iam_policy_document.registry_admin.json
}

resource "ccp_iam_role_assignment" "ci_can_push" {
  role_id        = ccp_iam_role.registry_admin.id
  principal_type = "service_account"
  principal_id   = ccp_service_account.ci.id
  expires_at     = "2027-05-10T00:00:00Z"
}
```

### Attach a built-in to an API key

```hcl
data "ccp_iam_role" "billing_reader" {
  name     = "BillingReader"
  built_in = true
}

resource "ccp_api_key" "finance_dashboard" {
  name   = "finance-readonly"
  scopes = ["read"]
}

resource "ccp_iam_role_assignment" "finance_billing" {
  role_id        = data.ccp_iam_role.billing_reader.id
  principal_type = "api_key"
  principal_id   = ccp_api_key.finance_dashboard.id
}
```

## Argument Reference

### Required

- `role_id` - (Required, Forces new resource) UUID of the role to attach.
- `principal_type` - (Required, Forces new resource) One of: `org_member`,
  `api_key`, `service_account`, `ccks_workload`.
- `principal_id` - (Required, Forces new resource) UUID of the principal.

### Optional

- `expires_at` - (Optional, Forces new resource) RFC 3339 expiry
  timestamp. After this point, the assignment is ignored during policy
  evaluation.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the assignment.
- `created_at` - RFC 3339 creation timestamp.

## Import

Assignments are imported using the composite ID `<role_id>/<assignment_id>`:

```
terraform import ccp_iam_role_assignment.ci_can_push <role_id>/<assignment_id>
```
