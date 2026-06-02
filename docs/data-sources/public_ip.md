---
page_title: "ccp_public_ip Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up a Public IP by ID, IP address, or label.
---

# ccp_public_ip (Data Source)

Look up a Public IP. Provide exactly one of `id`, `ip_address`, or `label`.

## Example Usage

```hcl
data "ccp_public_ip" "front" {
  ip_address = "203.0.113.10"
}

# Look up by its label (display name).
data "ccp_public_ip" "gw" {
  label = "passerelle-prod"
}

output "front_label" {
  value = data.ccp_public_ip.front.label
}

output "gw_ip" {
  value = data.ccp_public_ip.gw.ip_address
}
```

## Argument Reference

Provide exactly **one** of the following lookup keys:

- `id` - (Optional) UUID of the public IP.
- `ip_address` - (Optional) IPv4 address to look up.
- `label` - (Optional) Display name of the public IP to look up.

~> **Labels are not unique.** Several public IPs may carry the same `label`. If
more than one IP matches the requested label, the lookup fails with an explicit
error and you must disambiguate using `id` or `ip_address` instead.

## Attributes Reference

- `id`, `ip_address`, `pool_id`, `region`, `status`
- `label` - Display name of the IP, if set.
- `description` - Free-form description of the IP, if set.
- `container_id`, `vm_instance_id`, `load_balancer_id`, `load_balancer_name` (nullable)
- `created_at`
