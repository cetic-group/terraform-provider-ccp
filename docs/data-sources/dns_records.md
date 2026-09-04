---
page_title: "ccp_dns_records Data Source - ccp"
subcategory: "Networking"
description: |-
  Every record of a private DNS zone, including the ones the platform maintains.
---

# ccp_dns_records (Data Source)

Every record of a private DNS zone. Records maintained by the platform are listed too, flagged with
`is_system_managed` — they are read only, and they are what answers the apex of the zone.

## Example Usage

```hcl
data "ccp_dns_records" "corp" {
  zone_id = ccp_dns_zone.corp.id
}

output "my_records" {
  value = [
    for r in data.ccp_dns_records.corp.records : r
    if !r.is_system_managed
  ]
}
```

## Argument Reference

- `zone_id` - (Required) UUID of the zone (`ccp_dns_zone.id`).

## Attributes Reference

- `records` - The records of the zone. Each entry has:
  - `id` - UUID of the record.
  - `name` - Fully qualified name of the record.
  - `type` - Record type.
  - `ttl` - How long the answer may be cached, in seconds.
  - `records` - Values answered for this name and type.
  - `is_system_managed` - Whether the platform maintains this record.
  - `created_at` - RFC 3339 creation timestamp.
