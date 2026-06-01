---
page_title: "ccp_ssh_key Data Source - ccp"
subcategory: "Identity"
description: |-
  Look up an SSH key.
---

# ccp_ssh_key (Data Source)

Look up an SSH key by `id` or `name`.

## Example Usage

```hcl
data "ccp_ssh_key" "laptop" {
  name = "laptop"
}
```

## Attributes Reference

- `id`, `name`, `fingerprint`
- `scope` — `user`, `org`, or `tenant`
- `created_by_tenant_id`
- `created_at`
