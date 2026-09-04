---
page_title: "ccp_dns_record Resource - ccp"
subcategory: "Networking"
description: |-
  Manages one record of a private DNS zone — a name, a type, and all the values answered for it.
---

# ccp_dns_record (Resource)

Manages one record of a private DNS zone: a name, a type, and **all** the values answered for that
pair.

~> **`records` is the whole answer, not an addition.** Every apply sends the complete set — a value
removed from the configuration stops being answered. There is no incremental "add one value", and no
delta for the provider to compute.

`name` and `type` identify the record, so changing either replaces the resource. That is the correct
gesture: the platform has no route to rename a record.

## Example Usage

```hcl
resource "ccp_dns_record" "www" {
  zone_id = ccp_dns_zone.corp.id
  name    = "www"
  type    = "A"
  ttl     = 300
  records = ["10.20.0.10", "10.20.0.11"]
}

# `@` is the zone itself.
resource "ccp_dns_record" "apex_mx" {
  zone_id = ccp_dns_zone.corp.id
  name    = "@"
  type    = "MX"
  records = ["10 mail.corp.internal."]
}

# A TXT value carries its own quotes.
resource "ccp_dns_record" "spf" {
  zone_id = ccp_dns_zone.corp.id
  name    = "@"
  type    = "TXT"
  records = ["\"v=spf1 -all\""]
}
```

## Argument Reference

### Required

- `zone_id` - (Required, Forces new resource) UUID of the zone (`ccp_dns_zone.id`).
- `name` - (Required, Forces new resource) Name of the record, relative to the zone (`www`), fully
  qualified (`www.corp.internal`), or `@` for the zone itself. A relative name that happens to end
  with the zone name is still treated as relative: `intranet.corp` in `corp.internal` becomes
  `intranet.corp.corp.internal`. The fully qualified form is reported in `fqdn`.
- `type` - (Required, Forces new resource) One of `A`, `AAAA`, `CNAME`, `MX`, `TXT`, `SRV`, `CAA`.
- `records` - (Required) The values answered for this name and type, in presentation form —
  `10 mail.example.com.` for a `MX`, `"v=spf1 -all"` **with the quotes** for a `TXT`. 1 to 32 values,
  none of them padded with whitespace.

  A **set**, not a list: the values of a record are unordered, and the platform returns them in its
  own order. Ordering them would show a change at every `terraform plan` with nothing having moved.

### Optional

- `ttl` - (Optional, Computed) How long the answer may be cached, in seconds (60 to 604800). Defaults
  to 3600. Changed in place.

## Attributes Reference

- `id` - UUID of the record.
- `fqdn` - Fully qualified name of the record, as the platform stores it.
- `is_system_managed` - Whether the platform maintains this record. Always `false` for records
  created here; an imported platform record cannot be changed or deleted.
- `created_at` - RFC 3339 creation timestamp.

## Notes

- **`NS` is not offered.** The one at the apex is placed by the platform and is read only; anywhere
  else it is a delegation, which a private zone refuses. Read the apex record through the
  `ccp_dns_records` data source.
- **A zone will not delete while it carries records.** That is not a cascade — Terraform removes them
  first on its own when they are declared in the same configuration.

## Import

Records are imported by their identity, `<zone_id>/<name>/<type>` — the UUID is not something you
have at hand. `name` accepts the same forms as the attribute:

```
terraform import ccp_dns_record.www <zone_id>/www/A
terraform import ccp_dns_record.apex_mx <zone_id>/@/MX
```
