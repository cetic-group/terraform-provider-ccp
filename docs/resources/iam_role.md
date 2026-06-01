---
page_title: "ccp_iam_role Resource - ccp"
subcategory: "Identity"
description: |-
  Manages a custom CETIC Cloud IAM role (Roles v1) — AWS-style policy document with statements (effect, actions, resources, conditions).
---

# ccp_iam_role (Resource)

Manages a **custom** CETIC Cloud IAM role. A role bundles one or more policy
statements (Allow/Deny on `(action, resource ARN)` pairs) that can be
attached to a principal (org member, API key, service account, CCKS
workload) via [`ccp_iam_role_assignment`](./iam_role_assignment.md).

The 10 platform **built-in** roles — `AdminAll`, `ReadOnlyAll`, `Member`,
`RegistryAdmin`, `RegistryReader`, `BucketReader`, `BucketWriter`,
`K8sViewer`, `BillingReader`, `NetworkAdmin` — are seeded server-side and
not editable via Terraform. Look them up with the
[`ccp_iam_role` data source](../data-sources/iam_role.md).

~> **`policy_document_json` canonicalisation.** The API canonicalises your
input via a JCS RFC 8785-style algorithm and may re-emit keys in a different
order than you wrote them. The provider's `JSONNormalizeEqual` plan modifier
suppresses these spurious diffs — feed in any JSON shape and Terraform will
keep state stable. Use the [`ccp_iam_policy_document`](../data-sources/iam_policy_document.md)
data source for ergonomic HCL composition.

~> **Cross-tenant ARNs are rejected.** A custom role can only reference ARN
patterns whose `tenant_id` segment equals the caller's tenant or is the
wildcard `*`. Attempts to reference another tenant's ARN return a
`400 Bad Request`.

~> **Self-elevation is forbidden.** Custom roles cannot grant `iam:*`,
`iam:AttachRole`, `iam:CreateRole`, `iam:UpdateRole`, `iam:DeleteRole` or
`iam:DetachRole`. Only the read-side IAM actions (`iam:Get*`, `iam:List*`,
`iam:Simulate*`) are allowed.

## Example Usage

### Compose with `ccp_iam_policy_document`

```hcl
data "ccp_iam_policy_document" "registry_admin" {
  statement {
    sid       = "AllowRegistryAdmin"
    effect    = "Allow"
    actions   = ["registry:*"]
    resources = ["arn:ccp:registry:rnn:${var.tenant_id}:registry/myreg*"]
  }
  statement {
    sid       = "DenyRegistryDeletion"
    effect    = "Deny"
    actions   = ["registry:DeleteRegistry"]
    resources = ["*"]
  }
}

resource "ccp_iam_role" "registry_admin" {
  name                 = "RegistryAdminCustom"
  description          = "Manage all registries in RNN, except deletion."
  policy_document_json = data.ccp_iam_policy_document.registry_admin.json
}
```

### Inline JSON (less ergonomic, but works)

```hcl
resource "ccp_iam_role" "read_only_bucket" {
  name        = "BucketReadOnlyV2"
  description = "Read access to all buckets in the tenant."
  policy_document_json = jsonencode({
    version = "2026-05-10"
    statements = [{
      effect    = "Allow"
      actions   = ["bucket:Get*", "bucket:List*", "bucket:GetObject", "bucket:ListObjects"]
      resources = ["arn:ccp:bucket:*:${var.tenant_id}:*"]
    }]
  })
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name (1-64 chars). Must be unique
  within the tenant.
- `policy_document_json` - (Required) PolicyDocument as a JSON string —
  contains `version` (must equal `2026-05-10`) and a list of `statements`.
  Use [`ccp_iam_policy_document`](../data-sources/iam_policy_document.md)
  to compose it ergonomically.

### Optional

- `description` - (Optional) Free-form description (max 512 chars).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the role.
- `is_built_in` - Always `false` for resources managed via Terraform.
- `policy_hash` - SHA-256 hex of the canonical PolicyDocument — useful as
  a stable drift identifier (e.g. checking that two roles encode the same
  policy).
- `created_at` - RFC 3339 creation timestamp.
- `updated_at` - RFC 3339 timestamp of the last update.

## Import

IAM roles can be imported using their UUID:

```
terraform import ccp_iam_role.registry_admin <role_id>
```

Built-in roles cannot be imported as `ccp_iam_role` — use the
[data source](../data-sources/iam_role.md) instead.
