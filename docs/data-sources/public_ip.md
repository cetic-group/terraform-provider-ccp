---
page_title: "ccp_public_ip Data Source - cetic-cloud-platform"
subcategory: "Network"
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
```

## Attributes Reference

- `id`, `ip_address`, `pool_id`, `region`, `status`
- `container_id`, `vm_instance_id`, `load_balancer_id`, `load_balancer_name` (nullable)
- `created_at`
