---
page_title: "ccp_email_domain Resource - ccp"
subcategory: "Email"
description: |-
  Manages a hosted mail domain, and reports the DNS records to publish for it.
---

# ccp_email_domain (Resource)

Manages a hosted mail domain.

~> **The domain is created on hold.** It is not routed until you publish the record given in
`verification` in the public DNS of the domain, and the platform has seen it. Creating a
`ccp_email_account` before that fails — the mailbox would have nowhere to exist.

The usual sequence is two applies: the first declares the domain and hands you the records to
publish; the second, with `wait_for_verification = true`, activates it. When the DNS records are
managed by another provider in the same configuration, order them with `depends_on`.

## Example Usage

### First apply — declare and read the records to publish

```hcl
resource "ccp_email_domain" "example" {
  name = "example.com"
}

output "records_to_publish" {
  value = concat(
    [ccp_email_domain.example.verification],
    ccp_email_domain.example.dns_records,
  )
}
```

### Second apply — activate once the record is live

```hcl
resource "ccp_email_domain" "example" {
  name                  = "example.com"
  wait_for_verification = true
}
```

### With the public zone managed elsewhere

```hcl
resource "ccp_email_domain" "example" {
  name                  = "example.com"
  wait_for_verification = true

  # Whatever publishes the TXT record must come first.
  depends_on = [cloudflare_record.ccp_verification]
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Domain to host, e.g. `example.com`. A mail domain has a
  single owner, so the name is claimed platform-wide as soon as it is declared.

### Optional

- `wait_for_verification` - (Optional, Computed) `false` (the default) returns as soon as the domain
  is declared. `true` asks the platform to check the ownership record and waits until the domain is
  `active`. Set it on a **second** apply, once the record from `verification` is live.

## Attributes Reference

- `id` - UUID of the domain.
- `status` - `pending_verification` (name reserved, nothing routed), `active`, or `suspended`
  (routing cut by the platform; configuration and mailboxes are kept).
- `verified_at` - When ownership was established. Null while the domain waits.
- `dkim_generated_at` - When the signing key was created. Null means no key exists yet — distinct
  from a key that exists but is not published in your zone, which reads from `dns_records`.
- `externally_managed` - Whether the CCP console is read-only on this domain because it is driven
  from infrastructure code. Set by the platform, not by this resource: one domain has one control
  plane, and two of them always diverge.
- `accounts_count`, `aliases_count` - Number of mailboxes and aliases on the domain.
- `created_at` - RFC 3339 creation timestamp.
- `verification` - The `TXT` record proving you own the domain — the only one that blocks activation,
  which is why it is kept apart from `dns_records`. Same shape as an entry of `dns_records`.
- `dns_records` - The `MX`, SPF, DKIM and DMARC records expected in the public zone of the domain,
  each with the state observed there. Each entry has:
  - `type` - `MX`, `TXT`, `CNAME` or `TLSA`.
  - `name` - Name of the record to publish.
  - `value` - Exact value to publish, complete — a `MX` includes its priority.
  - `status` - What was observed, and the gesture it calls for: `ok` (nothing to do), `missing`
    (publish the line), `mismatch` (a different value is published; correct it), `conflict` (the zone
    carries several SPF records, which puts it in permanent error — merge them into the single line
    given in `value`), `over_lookup_limit` (the published value is right, but the zone as a whole
    exceeds the SPF lookup budget, so SPF fails for the entire domain).
  - `hostname`, `priority` - For a `MX`, the server and the priority separately, because DNS control
    panels ask for two fields. Always a pair; both null for other types, and both null for a `MX`
    that could not be split — use `value` then.
  - `exceeds_lookup_limit` - The value shown is not publishable as-is. Independent of `status`.
  - `purpose` - What the record is for, in one sentence.

  The list is what the platform can determine at read time: when the mail server cannot be reached,
  only the records that depend on the platform alone (SPF, DMARC) are returned.
- `client_config` - Settings to copy into a mail application: `incoming` and `outgoing` (each with
  `protocol`, `hostname`, `port`, `security`) and `username_hint`.

## Notes

- **Deleting a domain is not a cascade.** It is refused while the domain still carries mailboxes or
  aliases, because cascading would destroy mail. Terraform removes them first on its own when they
  are declared in the same configuration.
- The record collecting the domain's DMARC reports is created by the platform, as a mailbox flagged
  `is_system_managed`. It is not managed here and must not be declared.

## Import

```
terraform import ccp_email_domain.example <domain_id>
```
