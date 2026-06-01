---
page_title: "ccp_iam_policy_document Data Source - ccp"
subcategory: "Identity"
description: |-
  Renders a CETIC Cloud IAM PolicyDocument (Roles v1) from ergonomic HCL blocks — pure local transformation, no API call.
---

# ccp_iam_policy_document (Data Source)

Renders a CETIC Cloud IAM PolicyDocument from ergonomic HCL `statement {}`
and `condition {}` blocks. The output `json` attribute is a canonical JSON
string (sorted keys, UTF-8, no extra whitespace) — bit-for-bit reproducible
across runs given identical input. It plugs directly into
[`ccp_iam_role.policy_document_json`](../resources/iam_role.md).

This data source is a **pure local transformation** — no API call is
performed, so it does not require the provider to be reachable.

~> **Supported condition operators (v1):** `StringEquals`,
`StringNotEquals`, `StringLike`, `IpAddress`, `NotIpAddress`,
`DateGreaterThan`, `DateLessThan`.

~> **Supported condition keys (v1):** `SourceIp`, `RequestTime`,
`RequestRegion`, `ResourceTag`, `RequestTag`, `OrgId`, `ApiKeyPrefix`,
`PrincipalType`.

Policy variables (`${ccp:tenant_id}`) and `ForAllValues:` modifiers are
**not** supported in v1.

## Example Usage

### Single Allow statement

```hcl
data "ccp_iam_policy_document" "registry_pull" {
  statement {
    sid       = "AllowPull"
    effect    = "Allow"
    actions   = ["registry:Pull", "registry:Get*", "registry:List*"]
    resources = ["arn:ccp:registry:*:${var.tenant_id}:registry/*"]
  }
}
```

### Multi-statement with Deny override

```hcl
data "ccp_iam_policy_document" "registry_admin_no_delete" {
  statement {
    sid       = "AllowAll"
    effect    = "Allow"
    actions   = ["registry:*"]
    resources = ["arn:ccp:registry:*:${var.tenant_id}:*"]
  }
  statement {
    sid       = "DenyDeletion"
    effect    = "Deny"
    actions   = ["registry:DeleteRegistry"]
    resources = ["*"]
  }
}

resource "ccp_iam_role" "registry_admin" {
  name                 = "RegistryAdminNoDelete"
  policy_document_json = data.ccp_iam_policy_document.registry_admin_no_delete.json
}
```

### With conditions

```hcl
data "ccp_iam_policy_document" "office_only_push" {
  statement {
    sid       = "RestrictedPush"
    effect    = "Allow"
    actions   = ["registry:Push"]
    resources = ["arn:ccp:registry:*:${var.tenant_id}:registry/*"]

    condition {
      test     = "IpAddress"
      variable = "SourceIp"
      values   = ["203.0.113.0/24", "198.51.100.0/24"]
    }

    condition {
      test     = "DateLessThan"
      variable = "RequestTime"
      values   = ["2027-01-01T00:00:00Z"]
    }
  }
}
```

## Argument Reference

### Optional

- `version` - (Optional) Policy version. Defaults to `2026-05-10` (the
  only currently supported value).

### Blocks

- `statement` - (Required, repeatable) Each statement consists of:
  - `sid` - (Optional) Statement identifier (free-form short label).
  - `effect` - (Optional) `Allow` or `Deny`. Defaults to `Allow`.
  - `actions` - (Required) List of action strings, e.g. `registry:Push`.
    Wildcards allowed (`registry:*`, `*:Get*`).
  - `resources` - (Required) List of resource ARN patterns. `*` is
    allowed as a global wildcard.
  - `condition` - (Optional, repeatable) Each condition has `test`,
    `variable`, `values` — combined via the standard AWS-style semantics
    (`{ <test>: { <variable>: [<values>] } }`). Multiple conditions
    inside a statement are AND-combined.

## Attributes Reference

- `json` - Canonical JSON representation of the PolicyDocument. Stable
  across runs.
- `json_sha256` - Hex SHA-256 of the canonical JSON — useful as a stable
  identifier for the policy content (e.g. as a Terraform key).
