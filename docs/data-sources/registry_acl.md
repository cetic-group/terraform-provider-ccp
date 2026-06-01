---
page_title: "ccp_registry_acl Data Source - ccp"
subcategory: "Registry"
description: |-
  Look up a registry ACL by ID.
---

# ccp_registry_acl (Data Source)

Look up a CCR ACL by `(id, registry_id)`. Both are required.

## Attributes Reference

- `id`, `registry_id`, `user_id`, `username`, `repo_pattern`
- `actions` — list of granted actions (`pull`, `push`, `*`)
- `created_at`, `updated_at`
