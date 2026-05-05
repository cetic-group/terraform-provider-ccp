---
page_title: "ccp_vnet_ip_reservation Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a private IP reservation within a VNet on CETIC Cloud Platform.
---

# ccp_vnet_ip_reservation (Resource)

Manages a private IP reservation within a VNet. Reservations set aside specific IP addresses or contiguous ranges for dedicated use — useful for databases, load balancers, gateways, or any service that requires a stable, well-known private address.

~> **Note:** All arguments are immutable. Any change forces a new reservation to be created and the old one to be released.

## Example Usage

```hcl
# Reserve a specific IP for the primary database
resource "ccp_vnet_ip_reservation" "db_primary" {
  vnet_id = ccp_vnet.data.id
  label   = "db-primary"
  ip      = "10.0.2.10"
  purpose = "Primary PostgreSQL endpoint"
}

# Reserve a block of 8 consecutive IPs for a service mesh
resource "ccp_vnet_ip_reservation" "service_mesh" {
  vnet_id = ccp_vnet.web.id
  label   = "service-mesh-pool"
  count   = 8
  purpose = "Reserved for Envoy sidecar injection"
}
```

## Argument Reference

### Required

- `vnet_id` - (Required, Forces new resource) UUID of the VNet to reserve IPs in.
- `label` - (Required, Forces new resource) Human-readable label for the reservation.

### Optional

- `ip` - (Optional, Forces new resource) Specific IP address to reserve (e.g. `10.0.2.10`). Must be within the VNet CIDR. Mutually exclusive with `count`.
- `count` - (Optional, Forces new resource) Number of consecutive IPs to reserve as a range. Must be between 1 and 64. Cannot be used together with `ip`. Defaults to `1` when neither `ip` nor `count` is set.
- `purpose` - (Optional, Forces new resource) Free-text description of the reservation's intended use.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the reservation.
- `kind` - Whether the reservation is a `single` IP or a `range`.
- `created_at` - Timestamp of creation (RFC3339).

## Import

IP reservations can be imported using the VNet ID and reservation ID separated by a slash:

```
terraform import ccp_vnet_ip_reservation.db_primary <vnet_id>/<reservation_id>
```
