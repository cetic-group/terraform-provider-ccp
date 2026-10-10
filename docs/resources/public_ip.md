---
page_title: "ccp_public_ip Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a public IP address on CETIC Cloud Platform.
---

# ccp_public_ip (Resource)

Manages a public IP address allocated from a regional pool. Public IPs can be attached to a container or VM instance via `attached_to_id` + `attached_to_type`. Load-balancer attachment is handled by `ccp_load_balancer` and is surfaced here as read-only.

~> **Note:** IP attachment and detachment are asynchronous operations. The `status` attribute transitions through `attaching` / `detaching` before reaching `attached` / `allocated`. The provider polls until the transition completes (up to 5 minutes).

## Example Usage

```hcl
# Allocate a named public IP and attach it to a container
resource "ccp_public_ip" "web" {
  region           = "RNN"
  label            = "passerelle-prod"
  description      = "IP fixe du frontal web de production"
  attached_to_id   = ccp_container_instance.web.id
  attached_to_type = "container"
}

# Allocate without immediate attachment
resource "ccp_public_ip" "spare" {
  region = "RNN"
}

# Choose how the IP reaches the resource (types offered depend on the platform)
resource "ccp_public_ip" "behind_firewall" {
  region = "RNN"
  type   = "firewall"
}

# Allocate several IPs at once — use Terraform's native count
resource "ccp_public_ip" "api" {
  count  = 3
  region = "RNN"
  label  = "ip-fixe-api-${count.index + 1}"
}
```

## Argument Reference

### Required

- `region` - (Required, Forces new resource) Region where the IP is allocated. One of: `RNN` (Rennes, France), `PAR` (Paris, France), `ABJ` (Abidjan, Côte d'Ivoire).

### Optional

- `type` - (Optional) How the public IP reaches the resource:
  - `edge` — routed straight to your network; usable on every resource.
  - `firewall` — published by the platform firewall; for VMs and containers.
  - `nat` — 1:1 translation; the resource keeps its private address.

  Which types are offered depends on the platform: `cetic ip types --region <R>` lists them, and an unavailable type is rejected at creation. If omitted, the first offered type that still has free IPs in the region is used and exported here. Changing a configured type forces a new resource; leaving it unset, or adding it to an IP created before the attribute existed, never does — existing IPs are not replaced when upgrading the provider. On a platform that does not offer types yet, creating an IP with `type` is refused before anything is allocated.
- `pool_id` - (Optional, Forces new resource) UUID of the IP pool to allocate from. If omitted, the API picks the first available pool of the requested (or default) type in the region.
- `label` - (Optional) Display name for the IP (e.g. `passerelle-prod`, `ip-fixe-api`). Max 100 characters. Mutable in-place — changing it does not detach or recreate the IP. To remove a label, omit the attribute from the configuration.
- `description` - (Optional) Free-form description of what this IP is used for. Mutable in-place.
- `attached_to_id` - (Optional) UUID of the container or VM instance the IP should be attached to. Setting this attaches the IP; updating it re-attaches to a different resource. Must be paired with `attached_to_type`.
- `attached_to_type` - (Optional) Type of the resource referenced by `attached_to_id`. One of: `container`, `vm_instance`. Required when `attached_to_id` is set. Load-balancer attachment is not accepted here — use the `ccp_load_balancer` resource instead.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the public IP allocation.
- `ip_address` - The actual IPv4 address assigned by the pool.
- `status` - Current lifecycle state. One of: `available`, `allocated`, `attached`, `reserved`. `reserved` is a platform-managed lock and prevents release.
- `container_id` - UUID of the container this IP is attached to, if any.
- `vm_instance_id` - UUID of the VM instance this IP is attached to, if any.
- `load_balancer_id` - UUID of the load balancer this IP is attached to, if any. Read-only — set via `ccp_load_balancer`.
- `load_balancer_name` - Display name of the load balancer this IP is attached to, if any.
- `created_at` - RFC 3339 timestamp at which the IP was allocated.

## Import

Public IPs can be imported using their UUID:

```
terraform import ccp_public_ip.web <public_ip_id>
```
