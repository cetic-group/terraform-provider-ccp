---
page_title: "ccp_email_accounts Data Source - ccp"
subcategory: "Email"
description: |-
  The mailboxes of the organisation, optionally restricted to one domain.
---

# ccp_email_accounts (Data Source)

The mailboxes of the organisation, optionally restricted to one domain. Mailbox passwords never
appear here, in any shape: a mailbox password is reset, never read back.

## Example Usage

```hcl
data "ccp_email_accounts" "example" {
  domain_id = ccp_email_domain.example.id
}

output "mailboxes_over_80_percent" {
  value = [
    for a in data.ccp_email_accounts.example.accounts : a.address
    if a.usage_bytes != null && a.usage_bytes > a.quota_bytes * 0.8
  ]
}
```

## Argument Reference

- `domain_id` - (Optional) Restrict the list to this domain (`ccp_email_domain.id`). Omit for every
  mailbox of the organisation.

## Attributes Reference

- `accounts` - The mailboxes. Each entry has `id`, `address`, `quota_bytes`, `usage_bytes`,
  `usage_updated_at`, `enabled`, `enable_imap`, `enable_pop`, `is_system_managed`,
  `send_as_any_address`, `send_as_pending`, `forward_enabled`, `forward_destination`, `forward_keep`,
  `comment`, `displayed_name` and `created_at`. See
  [`ccp_email_account` (Resource)](../resources/email_account.md) for what each one means.

~> `usage_bytes` is a reading, not a live figure — read it together with `usage_updated_at`, which
can be hours old. Null means no reading has succeeded yet, which is not the same as an empty mailbox.
