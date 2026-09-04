---
page_title: "ccp_email_account Resource - ccp"
subcategory: "Email"
description: |-
  Manages a mailbox on a hosted mail domain.
---

# ccp_email_account (Resource)

Manages a mailbox on a hosted mail domain.

~> **The domain must already be active.** A mailbox on a domain still waiting for its proof of
ownership is refused — it would have nowhere to exist. Order the two with `depends_on` when the DNS
records come from another provider.

~> **`password` is write only.** The platform never returns it, so the value in your configuration is
the only copy Terraform has. Changing it here resets the mailbox password; a password changed outside
Terraform cannot be detected.

## Example Usage

```hcl
resource "ccp_email_account" "contact" {
  address        = "contact@example.com"
  password       = var.contact_password
  quota_gb       = 5
  displayed_name = "Sales"

  depends_on = [ccp_email_domain.example]
}

resource "ccp_email_account" "archive" {
  address  = "archive@example.com"
  password = var.archive_password

  forward_enabled     = true
  forward_destination = ["contact@example.com"]
  forward_keep        = true
}
```

## Argument Reference

### Required

- `address` - (Required, Forces new resource) Full address to create, e.g. `contact@example.com`. Its
  domain part designates the `ccp_email_domain`. Renaming a mailbox would move mail already delivered
  and break every forwarding rule pointing at it, hence the replacement.
- `password` - (Required, Sensitive) Mailbox password for IMAP, POP3 and SMTP, 12 to 128 characters.
  The floor is higher than the console's because a mailbox password is proved directly against IMAP
  and SMTP, which are open to the internet with no captcha in front.

### Optional

- `quota_gb` - (Optional) Space reserved for the mailbox, in gigabytes (1 to 1024). Omit to take the
  platform default. Changed in place, but **never below what the mailbox already holds** — the
  platform refuses that. Not computed on purpose: this is what you asked for, while `quota_bytes`
  reports what was actually reserved.
- `enabled` - (Optional, Computed) Whether the mailbox accepts logins and deliveries. Defaults to
  `true`. Turning it off deletes nothing and frees nothing: the space stays reserved, so it stays
  billed.
- `enable_imap` - (Optional, Computed) Whether IMAP is available. Defaults to `true`.
- `enable_pop` - (Optional, Computed) Whether POP3 is available. Defaults to `false`: POP3 downloads
  and deletes, which surprises anyone reading mail on more than one device.
- `comment` - (Optional, Computed) Free-form note about the mailbox (max 255 characters). Removing the attribute keeps the current note — the platform's update route has no way to clear a field, so set it to `""` instead.
- `displayed_name` - (Optional, Computed) Name shown as the sender, e.g. `Sales` (max 160 characters). Removing the attribute keeps the current name — set it to `""` to clear it.
- `forward_enabled` - (Optional, Computed) Whether incoming mail is forwarded to
  `forward_destination`.
- `forward_destination` - (Optional, Computed) Addresses mail is forwarded to, up to 20.
- `forward_keep` - (Optional, Computed) Whether forwarded mail is also kept in the mailbox. Setting
  this to `false` has consequences: without a local copy, a wrong destination loses the mail for
  good.

## Attributes Reference

- `id` - UUID of the mailbox.
- `quota_bytes` - Space actually reserved, in bytes — the billing basis. Never the space used.
- `usage_bytes` - Last reading of the space occupied. Null while no reading has succeeded, which is
  not the same as an empty mailbox.
- `usage_updated_at` - When `usage_bytes` was last read. Always read the two together: the figure can
  be hours old.
- `is_system_managed` - Whether the platform owns this mailbox — the address collecting the domain's
  DMARC reports is one. Always `false` for mailboxes created here.
- `send_as_any_address` - Whether the mailbox may send under any address of its domain. Read only
  here: it is a privilege of its own, granted through its own API call and its own permission, so
  that managing mailboxes can be delegated without it.
- `send_as_pending` - `true` when that privilege is recorded but not yet applied by the mail server —
  sending under another address is still refused until it converges.
- `created_at` - RFC 3339 creation timestamp.
- `client_config` - Settings to copy into a mail application, computed **for this mailbox**: the
  incoming server follows its POP3 flag. `incoming` and `outgoing` each carry `protocol`,
  `hostname`, `port` and `security`; `username_hint` reminds that the user name is the full
  address — entering only the local part is the first cause of failed setups.

## Import

```
terraform import ccp_email_account.contact <account_id>
```

`password` cannot be imported — the platform does not hold it. The first plan after an import
therefore shows a change on that attribute, and applying it resets the mailbox password to the
configured value. That is deliberate: the alternative would be a state claiming to know a secret it
does not.
