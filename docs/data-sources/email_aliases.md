---
page_title: "ccp_email_aliases Data Source - ccp"
subcategory: "Email"
description: |-
  The mail aliases of the organisation, optionally restricted to one domain.
---

# ccp_email_aliases (Data Source)

The mail aliases of the organisation, optionally restricted to one domain.

## Example Usage

```hcl
data "ccp_email_aliases" "example" {
  domain_id = ccp_email_domain.example.id
}

output "catch_alls" {
  value = [
    for a in data.ccp_email_aliases.example.aliases : a.address
    if a.wildcard
  ]
}
```

## Argument Reference

- `domain_id` - (Optional) Restrict the list to this domain (`ccp_email_domain.id`). Omit for every
  alias of the organisation.

## Attributes Reference

- `aliases` - The aliases. Each entry has `id`, `address`, `destinations`, `wildcard`, `comment` and
  `created_at`.
