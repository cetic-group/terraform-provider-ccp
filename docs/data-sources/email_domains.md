---
page_title: "ccp_email_domains Data Source - ccp"
subcategory: "Email"
description: |-
  Every hosted mail domain of the organisation.
---

# ccp_email_domains (Data Source)

Every hosted mail domain of the organisation.

The list shape carries no expected DNS records and no mail-client settings; use the
[`ccp_email_domain`](email_domain.md) data source for one domain when those are needed.

## Example Usage

```hcl
data "ccp_email_domains" "all" {}

output "domains_waiting_for_verification" {
  value = [
    for d in data.ccp_email_domains.all.domains : d.name
    if d.status == "pending_verification"
  ]
}
```

## Attributes Reference

- `domains` - The mail domains. Each entry has `id`, `name`, `status`, `verified_at`,
  `dkim_generated_at`, `externally_managed`, `accounts_count`, `aliases_count` and `created_at`.
