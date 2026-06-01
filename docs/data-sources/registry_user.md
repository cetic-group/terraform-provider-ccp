---
page_title: "ccp_registry_user Data Source - ccp"
subcategory: "Registry"
description: |-
  Look up a registry user by username.
---

# ccp_registry_user (Data Source)

Look up a CCR user by `(username, registry_id)`. Both are required.

~> The password is NEVER exposed — only returned at creation time on the `ccp_registry_user` resource.

## Attributes Reference

- `id`, `registry_id`, `username`, `is_admin`, `created_at`
