---
page_title: "ccp_registry_user Resource - cetic-cloud-platform"
subcategory: "Registry"
description: |-
  Manages a non-admin user of a CETIC Container Registry.
---

# ccp_registry_user (Resource)

Manages a non-admin user account on a CETIC Container Registry. The user
authenticates via `docker login <registry hostname>` using `username` and
the one-shot `password` returned at creation. Combine with
[`ccp_registry_acl`](registry_acl.md) to grant pull/push permissions on
specific repository patterns.

The auto-provisioned **admin** user (used for break-glass access) is owned
by the [`ccp_registry`](registry.md) resource and does not need to be
declared here.

~> **Important — `password`:** The `password` attribute is delivered **only
at creation** by the API. It is written to the Terraform state and cannot
be retrieved afterwards. Treat your state file as sensitive. To rotate it,
`terraform taint` this resource — the destroy + create cycle issues a new
password.

All attributes of this resource force replacement (no in-place update).

## Example Usage

```hcl
resource "ccp_registry" "main" {
  name     = "ccr-prod"
  region   = "RNN"
  vpc_id   = ccp_vpc.main.id
  vnet_id  = ccp_vnet.registry.id
  exposure = "private"
}

resource "ccp_registry_user" "ci_pull" {
  registry_id = ccp_registry.main.id
  username    = "ci-pull"
}

# Grant pull access on every repo
resource "ccp_registry_acl" "ci_pull_all" {
  registry_id  = ccp_registry.main.id
  user_id      = ccp_registry_user.ci_pull.id
  repo_pattern = "*"
  actions      = ["pull"]
}

output "ci_pull_password" {
  value     = ccp_registry_user.ci_pull.password
  sensitive = true
}
```

## Argument Reference

### Required

- `registry_id` - (Required, Forces new resource) UUID of the parent `ccp_registry`.
- `username` - (Required, Forces new resource) Login username. Lowercase letters,
  digits and `-` only (1-32 chars). Must be unique within the registry.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the registry user.
- `is_admin` - Whether this user is the auto-provisioned registry admin
  (always `false` for users created via this resource).
- `password` - (Sensitive) Generated password, returned **only at creation**.
  Stored in the Terraform state.
- `created_at` - RFC 3339 creation timestamp.

## Import

Registry users can be imported using `<registry_id>/<user_id>`:

```
terraform import ccp_registry_user.ci_pull <registry_id>/<user_id>
```

~> **Note:** After import, `password` is unset in state — it cannot be
retrieved from the API. To recover the credential, taint the resource.
