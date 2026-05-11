# `examples/iam-roles` — IAM Roles v1 end-to-end

Composes a small but representative IAM setup that exercises every Roles v1
building block:

- A **custom role** `RegistryAdminNoDelete` rendered from an ergonomic
  `ccp_iam_policy_document` with both Allow and Deny statements.
- A **service account** `ci-pipeline` (token-based, `ccp_sa_*`) for CI/CD.
- An **assignment** wiring the custom role to that service account.
- A **lookup** of the built-in `BillingReader` role via the
  `ccp_iam_role` data source, attached to a separate API key for the
  finance team's read-only dashboard.

## Usage

```bash
export CCP_API_KEY="ccp_live_..."

# Find your tenant UUID — required to scope custom role ARNs:
#   cetic auth whoami | jq -r .tenant_id
export TF_VAR_tenant_id="11111111-2222-3333-4444-555555555555"
# Optional: override the default region (rnn|par|abj).
export TF_VAR_region="rnn"

terraform init
terraform apply
```

After apply, capture the one-shot service-account token (the API never
re-emits it):

```bash
terraform output -raw ci_sa_token > ci-sa.token
chmod 600 ci-sa.token
# Paste into the CI runner secret store, then delete the file.
```

To attach more principals (other API keys, additional service accounts,
CCKS workloads), add further `ccp_iam_role_assignment` resources — see
[`docs/resources/iam_role_assignment.md`](../../docs/resources/iam_role_assignment.md).

## What the custom role lets `ci-pipeline` do

The role contains two statements; the second's `Deny` wins over the
first's wildcard `Allow` for `registry:DeleteRegistry`:

| Action                       | Result   |
|------------------------------|----------|
| `registry:Push`              | ✅ Allow |
| `registry:Pull`              | ✅ Allow |
| `registry:GarbageCollect`    | ✅ Allow |
| `registry:CreateRegistry`    | ✅ Allow |
| `registry:DeleteRegistry`    | ❌ Deny  |
| `bucket:GetObject`           | ❌ Implicit deny (out of scope) |

Use `cetic iam simulate --principal-type service_account --principal-id $SA_ID --action registry:DeleteRegistry --resource '*'` to confirm.

## Rotating the service account token

Service-account tokens are revealed only at creation. To rotate:

```bash
terraform apply -replace=ccp_service_account.ci
```

This destroys + re-creates the SA, issues a fresh token, and re-applies
the existing role assignment.

## Cleanup

```bash
terraform destroy
```
