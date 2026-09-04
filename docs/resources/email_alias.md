---
page_title: "ccp_email_alias Resource - ccp"
subcategory: "Email"
description: |-
  Manages a mail alias — one source address rewritten to one or more destinations.
---

# ccp_email_alias (Resource)

Manages a mail alias: one source address, rewritten to one or more destinations at delivery. An alias
stores nothing — it is not a mailbox, it has no quota and costs nothing — and several destinations
make it a distribution group.

## Example Usage

```hcl
resource "ccp_email_alias" "team" {
  address = "team@example.com"
  destinations = [
    "alice@example.com",
    "bob@example.com",
    "consultant@partner.example",
  ]
  comment = "Shared team address"
}

resource "ccp_email_alias" "catch_all" {
  address      = "*@example.com"
  destinations = ["contact@example.com"]
  wildcard     = true
}
```

## Argument Reference

### Required

- `address` - (Required, Forces new resource) Source address, e.g. `contact@example.com`, or
  `*@example.com` for a catch-all — which also requires `wildcard = true`. Changing it is a different
  alias, hence the replacement.
- `destinations` - (Required) Addresses mail is delivered to, inside or outside the platform. 1 to
  100 entries. Changed in place; the list replaces the previous one whole.

### Optional

- `wildcard` - (Optional, Computed) Catch-all: takes every message addressed to an address of the
  domain that does not exist. Defaults to `false`, and deliberately opt-in — it also takes dictionary
  spam, and it hides for good the typos that should have bounced.
- `comment` - (Optional, Computed) Free-form note about the alias (max 255 characters). Removing the attribute keeps the current note — set it to `""` to clear it.

## Attributes Reference

- `id` - UUID of the alias.
- `created_at` - RFC 3339 creation timestamp.

## Notes

Declaring `*@example.com` with `wildcard = false` is rejected at plan time. The two would be read
differently by the platform and by the mail server: you would believe the catch-all is off while it
is on.

## Import

```
terraform import ccp_email_alias.team <alias_id>
```
