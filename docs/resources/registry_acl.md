---
page_title: "ccp_registry_acl Resource - ccp"
subcategory: "Registry"
description: |-
  Grants a CETIC Container Registry user permissions on a repository pattern.
---

# ccp_registry_acl (Resource)

Grants a `ccp_registry_user` a set of `actions` over a repository name
pattern. Each ACL maps to one rule in the registry's
`cesanta/docker_auth` configuration. Patterns use shell-style globs (e.g.
`myapp/*` matches `myapp/web`, `myapp/worker`, …; `*` matches any
repository).

`repo_pattern` and `actions` are mutable in place — only `registry_id` and
`user_id` force replacement.

## Example Usage

A common pattern: an admin user with `*:*` and a CI user constrained to
push to a single namespace.

```hcl
resource "ccp_registry" "main" {
  name           = "ccr-prod"
  region         = "RNN"
  expose_public  = false
  expose_private = true
}

resource "ccp_registry_user" "alice" {
  registry_id = ccp_registry.main.id
  username    = "alice"
}

resource "ccp_registry_user" "ci" {
  registry_id = ccp_registry.main.id
  username    = "ci-pipeline"
}

# Alice has full access to everything
resource "ccp_registry_acl" "alice_all" {
  registry_id  = ccp_registry.main.id
  user_id      = ccp_registry_user.alice.id
  repo_pattern = "*"
  actions      = ["*"]
}

# CI can push only to myapp/*
resource "ccp_registry_acl" "ci_push" {
  registry_id  = ccp_registry.main.id
  user_id      = ccp_registry_user.ci.id
  repo_pattern = "myapp/*"
  actions      = ["pull", "push"]
}
```

## Argument Reference

### Required

- `registry_id` - (Required, Forces new resource) UUID of the parent `ccp_registry`.
- `user_id` - (Required, Forces new resource) UUID of the `ccp_registry_user` this rule applies to.
- `repo_pattern` - (Required) Repository name pattern. Lowercase letters,
  digits, `-`, `_`, `/` and `*` (1-255 chars).
- `actions` - (Required) Set of actions. Subset of:
  - `pull` — read images.
  - `push` — write images (implies pull on the same pattern when used together).
  - `*` — admin-equivalent on this pattern.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the ACL rule.
- `username` - Convenience read-back of the user's login name.
- `created_at` - RFC 3339 creation timestamp.
- `updated_at` - RFC 3339 timestamp of the most recent edit.

## Import

ACLs can be imported using `<registry_id>/<acl_id>`:

```
terraform import ccp_registry_acl.ci_push <registry_id>/<acl_id>
```
