---
page_title: "ccp_dns_zone Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up a private DNS zone by id or by name.
---

# ccp_dns_zone (Data Source)

Look up a private DNS zone by `id` **or** by `name` — exactly one of the two. Zone names are unique
within your organisation.

The usual reason to reach for it is `resolver_endpoints`: the address to hand to a machine as its
name server, and the subnet that address belongs to.

## Example Usage

```hcl
data "ccp_dns_zone" "corp" {
  name = "corp.internal"
}

output "office_name_server" {
  value = one([
    for e in data.ccp_dns_zone.corp.resolver_endpoints : e.address
    if e.vnet_id == ccp_vnet.office.id
  ])
}
```

## Argument Reference

- `id` - (Optional) UUID of the zone. Conflicts with `name`.
- `name` - (Optional) Name of the zone, e.g. `corp.internal`. Conflicts with `id`.

## Attributes Reference

- `vpc_id` - UUID of the private network the zone is served in.
- `region` - Region the zone is served from.
- `status` - `pending_verification`, `provisioning`, `active` or `error`.
- `default_ttl` - Default time-to-live of the zone, in seconds.
- `dnssec_enabled` - Whether the zone is signed.
- `error_message` - Why the zone could not be brought up, when `status` says so.
- `record_sets_count` - Number of records you declared; the apex record the platform maintains is not
  counted.
- `created_at` - RFC 3339 creation timestamp.
- `resolver_addresses` - Addresses to use as name server from this network, one per subnet served.
- `resolver_endpoints` - The same addresses, each with the subnet it serves: `address`, `vnet_id`,
  `vnet_name`, `vnet_cidr`.
- `resolver_tier` - Service level serving the network: `dev` or `prod`.
- `resolver_status` - State of the name server itself.
- `ns_hostname` - Name of the server published at the apex of the zone.
- `applies_to_new_guests_only` - Machines receive the name server when they are created; existing
  ones keep theirs.
- `ownership_challenge` - `record_name`, `record_type`, `record_value` — the record to publish in the
  public DNS of the domain. Null on an internal suffix and once the proof is accepted.
