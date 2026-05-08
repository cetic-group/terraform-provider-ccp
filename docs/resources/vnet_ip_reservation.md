---
page_title: "ccp_vnet_ip_reservation Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a private IP reservation within a VNet on CETIC Cloud Platform.
---

# ccp_vnet_ip_reservation (Resource)

Manages a private IP reservation within a VNet. Reservations set aside specific IP addresses or contiguous ranges (via `range_end`) for dedicated use — useful for databases, load balancers, gateways, or any service that requires a stable, well-known private address.

~> **Note:** All arguments are immutable. Any change forces a new reservation to be created and the old one to be released.

## Example Usage

```hcl
# Reserve a specific IP for the primary database
resource "ccp_vnet_ip_reservation" "db_primary" {
  vnet_id     = ccp_vnet.data.id
  name        = "db-primary"
  ip          = "10.0.2.10"
  description = "Primary PostgreSQL endpoint"
}

# Reserve a block of 8 consecutive IPs (10.0.1.20 → 10.0.1.27) for a service mesh
resource "ccp_vnet_ip_reservation" "service_mesh" {
  vnet_id     = ccp_vnet.web.id
  name        = "service-mesh-pool"
  ip          = "10.0.1.20"
  range_end   = "10.0.1.27"
  description = "Reserved for Envoy sidecar injection"
}
```

## Argument Reference

### Required

- `vnet_id` - (Required, Forces new resource) UUID of the VNet to reserve IPs in.
- `name` - (Required, Forces new resource) Human-readable label for the reservation.
- `ip` - (Required, Forces new resource) First IPv4 address of the reservation. Must be within the VNet CIDR. For a single IP, omit `range_end`.

### Optional

- `range_end` - (Optional, Forces new resource) Last IPv4 address of the range (inclusive). Must be ≥ `ip` and within the same VNet CIDR. Omit for a single-IP reservation.
- `description` - (Optional, Forces new resource) Free-text description of the reservation's intended use.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the reservation.
- `ip_count` - Number of IPs covered by the reservation (`1` for single, `range_end - ip + 1` for a range). Renamed from `count` in v0.7.1 (collision with the Terraform meta-argument).
- `kind` - Whether the reservation is a `single` IP or a `range`.
- `created_at` - Timestamp of creation (RFC3339).

## Import

Reservations can be imported using their UUID:

```
terraform import ccp_vnet_ip_reservation.db_primary <reservation_id>
```
