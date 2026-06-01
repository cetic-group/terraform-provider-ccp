---
page_title: "ccp_appgw_target_group_member Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a single backend (member) inside an Application Gateway target group.
---

# ccp_appgw_target_group_member (Resource)

Manages a single backend (member) inside a [`ccp_appgw_target_group`](appgw_target_group.md). Exactly one of `container_id`, `vm_instance_id` or `target_ip` must be set — checked at plan-time via `ValidateConfig`.

~> **`appgw_id`, `target_group_id`, the target identifier and `port` are immutable.** Any change forces a destroy + create. `weight` and `enabled` can be changed in place.

## Example Usage — Container backend

```hcl
resource "ccp_appgw_target_group_member" "api_01" {
  appgw_id        = ccp_application_gateway.web.id
  target_group_id = ccp_appgw_target_group.api_pool.id
  container_id    = ccp_container_instance.api_01.id
  port            = 8080
  weight          = 100
  enabled         = true
}
```

## Example Usage — Raw IP backend

```hcl
resource "ccp_appgw_target_group_member" "external" {
  appgw_id        = ccp_application_gateway.web.id
  target_group_id = ccp_appgw_target_group.api_pool.id
  target_ip       = "10.0.1.50"
  port            = 8080
}
```

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `target_group_id` - (Required, Forces new resource) UUID of the parent target group.
- `port` - (Required, Forces new resource) Backend port (1-65535).

### Required (one of)

- `container_id` - (Optional, Forces new resource) UUID of a `ccp_container_instance` used as backend.
- `vm_instance_id` - (Optional, Forces new resource) UUID of a `ccp_vm_instance` used as backend.
- `target_ip` - (Optional, Forces new resource) Raw IPv4/IPv6 address inside the gateway VNet used as backend.

### Optional

- `weight` - (Optional, default `100`) Load-balancing weight (0-1000). 0 drains the backend.
- `enabled` - (Optional, default `true`) When `false`, the backend is administratively disabled (useful for manual drain).

## Attributes Reference

- `id` - UUID of the member.

## Import

```
terraform import ccp_appgw_target_group_member.api_01 <appgw_id>/<target_group_id>/<member_id>
```
