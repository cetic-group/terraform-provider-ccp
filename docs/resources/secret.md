---
page_title: "ccp_secret Resource - ccp"
subcategory: "Secret Manager"
description: |-
  Manages an encrypted secret in the CETIC Cloud Secret Manager — a generic key/value blob, K8s-agnostic.
---

# ccp_secret (Resource)

Manages a secret in the **CETIC Cloud Secret Manager**. A secret holds an
encrypted `string → string` map (AES-256-GCM at rest) — it is a generic
vault entry, **K8s-agnostic** by design. When projecting it into a
Kubernetes cluster via the `CCPSecret` CRD, the native Kubernetes Secret
type (`Opaque`, `kubernetes.io/tls`, …) is specified on the CRD at the
workload cluster — not on this resource.

~> **`data` is `Sensitive` and stored in Terraform state.** Plaintext
values are persisted in your state backend — keep it encrypted at rest
with restricted access (S3 SSE-KMS, Terraform Cloud workspace, etc.).

~> **Drift on `data` is NOT detected.** The CETIC Cloud reveal endpoint
(`/v1/secrets/{id}/value`) is audit-logged and rate-limited, so the
provider does not call it on every `terraform refresh`. The state holds
the plaintext from the most recent Create / Update. If you suspect
out-of-band rotation, either change `data` in the config (which triggers
a fresh rotate) or taint the resource and re-apply.

~> **`name` is immutable.** Changing it forces destroy + create (the
API also rejects the change with `422` — the provider surfaces it as a
clean `RequiresReplace` plan modifier).

~> **`description` and `tags` are mutable in place** via `PATCH`. When
both `data` and metadata change in the same plan, the provider issues
the `rotate` call first and the `PATCH` second — this keeps the audit
trail ordered (version bump precedes metadata edit).

## Example Usage

### Simple secret (single password value)

```hcl
resource "ccp_secret" "db_password" {
  name = "prod-db-password"
  data = {
    password = "p@ssw0rd"
  }
  tags = ["env:prod", "team:platform"]
}
```

### Secret carrying files (e.g. TLS keypair)

The native Kubernetes Secret type (`kubernetes.io/tls`, etc.) is **not**
specified here — it is decided on the `CCPSecret` CRD applied inside the
workload cluster. From the platform's point of view this is just a
key/value blob.

```hcl
resource "ccp_secret" "wildcard_tls" {
  name        = "wildcard-cetic-tls"
  description = "Wildcard TLS keypair — type set on the CCPSecret CRD"
  data = {
    "tls.crt" = file("./certs/wildcard.crt")
    "tls.key" = file("./certs/wildcard.key")
  }
  tags = ["env:prod"]
}
```

## Argument Reference

### Required

- `name` - (Required, ForceNew) DNS-friendly secret name unique within
  the org. Must match `^[a-z][a-z0-9-]{0,62}$`. Changing forces
  replacement.
- `data` - (Required, Sensitive) Map of plaintext `string → string`
  key/value pairs to encrypt and store. Changing `data` triggers a
  server-side rotation (`POST /v1/secrets/{id}/rotate`) and bumps
  `version`.

### Optional

- `description` - (Optional) Free-form description (max 500 chars).
- `tags` - (Optional) Free-form list of tags for organising secrets
  (e.g. `["env:prod", "team:platform"]`). Mutable in place.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - Server-assigned UUID of the secret.
- `version` - Monotonic version counter bumped on every rotation.
- `created_at` - RFC 3339 creation timestamp.
- `updated_at` - RFC 3339 timestamp of the last metadata edit or rotation.

## Import

Secrets are imported using their UUID:

```
terraform import ccp_secret.db_creds <secret_id>
```

~> **Note:** After import, `data` is `null` in state — the provider does
not implicitly call the audit-logged reveal endpoint. Run
`terraform apply` with the expected `data` to reconcile (this will issue
a `rotate` call, which bumps `version`). If you absolutely need to import
without rotating, fetch the current values via `cetic secret value <id>`
and place them in your config before applying.
