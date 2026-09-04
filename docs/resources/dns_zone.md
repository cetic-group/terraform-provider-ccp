---
page_title: "ccp_dns_zone Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a private DNS zone, answered only inside your own private network.
---

# ccp_dns_zone (Resource)

Manages a private DNS zone. The zone is answered **only inside the private network you attach it
to** — it is never published to the internet — and the machines of that network receive its name
server automatically.

~> **The service level is a property of the network, not of the zone.** All zones of the same
`vpc_id` are answered by the same name server, so they share its `tier`. Declaring a second zone in
that network with a different `tier` is rejected; the error names the level already in place.

~> **Machines already running keep the name server they were given at creation.** Turning private
DNS on in a populated network does not make the zone visible from the existing machines — create the
zone before the machines, or recreate them afterwards. This is what `applies_to_new_guests_only`
reports.

## Example Usage

### Internal name — the common case

```hcl
resource "ccp_vpc" "main" {
  name   = "corp"
  region = "RNN"
}

resource "ccp_vnet" "office" {
  vpc_id = ccp_vpc.main.id
  name   = "office"
  cidr   = "10.20.0.0/24"
}

resource "ccp_dns_zone" "corp" {
  name        = "corp.internal"
  vpc_id      = ccp_vpc.main.id
  tier        = "prod"
  default_ttl = 300

  # The subnets must exist before the zone: the name server places one address
  # in each of them.
  depends_on = [ccp_vnet.office]
}

output "name_servers" {
  description = "Address to use from each subnet."
  value       = ccp_dns_zone.corp.resolver_endpoints
}
```

An internal suffix (`corp.internal`, `home.arpa`, `lan`, or a single label such as `corp`) needs no
proof of ownership: the zone goes straight to `active`.

### Public domain name — two applies

A **public** domain name (`example.com`) is created on hold until you prove you own it. Apply once
to obtain the record, publish it, then flip `wait_for_verification`:

```hcl
resource "ccp_dns_zone" "public" {
  name   = "example.com"
  vpc_id = ccp_vpc.main.id

  # First apply: leave at false — it returns immediately with the record below.
  # Second apply, once the record is live: set to true.
  wait_for_verification = false
}

output "publish_this_record" {
  value = ccp_dns_zone.public.ownership_challenge
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Zone name, e.g. `corp.internal`. Normalised to lower case
  by the platform. Unique within your organisation — two organisations may each hold `corp.internal`
  without seeing one another.
- `vpc_id` - (Required, Forces new resource) UUID of the private network (`ccp_vpc.id`) the zone is
  served in. This is the **private network**, not one of its subnets: the name server answers the
  same zones in every subnet. A network with more than nine subnets cannot be served, and creation
  is rejected rather than leaving part of the machines without name resolution.

### Optional

- `tier` - (Optional, Computed, Forces new resource) `dev` (single server) or `prod` (redundant name
  server with automatic failover). Defaults to `dev`. **Shared by every zone of `vpc_id`.**
- `default_ttl` - (Optional, Computed, Forces new resource) Default time-to-live of the zone, in
  seconds (60 to 604800). Omit to take the platform default (3600 s); the effective value is read
  back.
- `dnssec_enabled` - (Optional, Computed, Forces new resource) Signs the zone. Defaults to `false`:
  on a private zone there is no chain of trust from the public root, so it protects against very
  little.
- `wait_for_verification` - (Optional, Computed) Only meaningful for a **public** domain name. `false`
  (the default) returns as soon as the zone is declared; `true` asks the platform to check the
  ownership record and waits until the zone is answered. See the example above.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the zone.
- `status` - `pending_verification`, `provisioning`, `active`, or `error`. `error` is terminal:
  delete the zone and declare it again.
- `region` - Region the zone is served from — that of its private network.
- `error_message` - Why the zone could not be brought up, when `status` says so.
- `record_sets_count` - Number of records **you** declared. The apex record the platform maintains is
  not counted.
- `created_at` - RFC 3339 creation timestamp.
- `resolver_addresses` - Addresses to use as name server from this network — **one per subnet
  served**, empty until the name server is up. From a machine, use the address of ITS OWN subnet:
  they all answer the same zones, but each one is only reachable from its own subnet.
- `resolver_endpoints` - The same addresses, each with the subnet it serves. Read this one when the
  network has more than one subnet — an address taken from the wrong subnet does not answer. Each
  entry has `address`, `vnet_id`, `vnet_name` (may be empty) and `vnet_cidr`.
- `resolver_tier` - Service level actually serving the network. While the zone is still
  `pending_verification` this reports the level you *asked for*, not one that is running.
- `resolver_status` - State of the name server itself: `provisioning`, `active` or `error`. Observed
  directly, never derived from the state of the zone.
- `ns_hostname` - Name of the server published at the apex of the zone. Informational: it only
  resolves through that name server.
- `applies_to_new_guests_only` - Always `true`. See the note at the top of this page.
- `ownership_challenge` - For a public domain name still on hold, the record to publish in its public
  DNS: `record_name`, `record_type` (always `TXT`) and `record_value`. Null on an internal suffix,
  and null again once the proof has been accepted.

## Import

DNS zones are imported by UUID:

```
terraform import ccp_dns_zone.corp <zone_id>
```

~> **Declare `tier` and `dnssec_enabled` explicitly before importing a zone that does not use their
defaults.** Both are `Optional + Computed` with a default, so a configuration that omits them plans
`dev` and `false` — and both force replacement. On a zone served at `prod`, the first plan after the
import would propose to destroy and recreate it. Read the zone's actual values from
`resolver_tier` and `dnssec_enabled` (the `ccp_dns_zone` data source shows both without importing
anything) and write them into the configuration first.
