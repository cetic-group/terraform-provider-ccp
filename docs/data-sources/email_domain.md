---
page_title: "ccp_email_domain Data Source - ccp"
subcategory: "Email"
description: |-
  Look up a hosted mail domain by id or by name, with the DNS records expected for it.
---

# ccp_email_domain (Data Source)

Look up a hosted mail domain by `id` **or** by `name` — exactly one of the two.

The usual reason to reach for it is `verification` and `dns_records`: the records to publish in the
public zone of the domain, and the state observed there.

## Example Usage

```hcl
data "ccp_email_domain" "example" {
  name = "example.com"
}

output "records_still_missing" {
  value = [
    for r in data.ccp_email_domain.example.dns_records : r
    if r.status != "ok"
  ]
}
```

## Argument Reference

- `id` - (Optional) UUID of the domain. Conflicts with `name`.
- `name` - (Optional) Domain name, e.g. `example.com`. Conflicts with `id`.

## Attributes Reference

- `status` - `pending_verification`, `active` or `suspended`.
- `verified_at` - When ownership was established.
- `dkim_generated_at` - When the signing key was created.
- `externally_managed` - Whether the console is read-only on this domain because it is driven from
  infrastructure code.
- `accounts_count`, `aliases_count` - Number of mailboxes and aliases on the domain.
- `created_at` - RFC 3339 creation timestamp.
- `verification` - The `TXT` record proving you own the domain — the only one that blocks activation.
- `dns_records` - The `MX`, SPF, DKIM and DMARC records expected in the public zone, each with the
  state observed there. Each entry carries `type`, `name`, `value`, `status`, `hostname` and
  `priority` (a pair, `MX` only), `exceeds_lookup_limit` and `purpose`. See
  [`ccp_email_domain` (Resource)](../resources/email_domain.md) for the meaning of each
  `status`.
- `client_config` - Settings to copy into a mail application: `incoming` and `outgoing` (each with
  `protocol`, `hostname`, `port`, `security`) and `username_hint`.
