---
page_title: "ccp_public_ip Data Source - ccp"
subcategory: "Networking"
description: |-
  Look up a Public IP by ID or by IP address.
---

# ccp_public_ip (Data Source)

Look up a Public IP. Provide exactly one of `id` or `ip_address`.

## Example Usage

```hcl
data "ccp_public_ip" "front" {
  ip_address = "203.0.113.10"
}

output "front_label" {
  value = data.ccp_public_ip.front.label
}
```

## Argument Reference

- `id` - (Optional) UUID of the public IP. Conflicts with `ip_address`.
- `ip_address` - (Optional) IPv4 address to look up. Conflicts with `id`.

## Attributes Reference

- `id`, `ip_address`, `pool_id`, `region`, `status`
- `label` - Display name of the IP, if set.
- `description` - Free-form description of the IP, if set.
- `container_id`, `vm_instance_id`, `load_balancer_id`, `load_balancer_name` (nullable)
- `created_at`
