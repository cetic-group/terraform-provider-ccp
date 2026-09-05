---
page_title: "ccp_container_scale_set Data Source - ccp"
subcategory: "Compute"
description: |-
  Look up a container scale set.
---

# ccp_container_scale_set (Data Source)

Look up a container scale set by `id` or `(name, region)`.

## Attributes Reference

- `id`, `name`, `region`, `plan`, `template`, `vnet_id` (nullable)
- `min_instances`, `max_instances`, `desired_instances`, `auto_repair`
- `status`, `error_message` (nullable)
- `tags`, `created_at`, `updated_at`

- `containers` - The scale set's members as the platform knows them **at read time**. Each entry carries `id`, `name`, `status` and `ip_address` (`null` while a member has no address yet). Use it to place a scale set behind an Application Gateway or a load balancer:

```hcl
data "ccp_container_scale_set" "prod" {
  id = ccp_container_scale_set.prod.id
}

resource "ccp_appgw_target_group_member" "prod" {
  for_each        = { for m in data.ccp_container_scale_set.prod.containers : m.id => m }
  appgw_id        = ccp_application_gateway.prod.id
  target_group_id = ccp_appgw_target_group.prod.id
  container_id      = each.value.id
  port            = 8080
}
```

~> **This set drifts as soon as the size changes.** A member added or removed outside Terraform is not in state, and the next `plan` will offer to adjust the backends accordingly. It is usable on a fixed-size set; a set that actually scales needs a `scale_set_id` target reconciled by the platform, which does not exist yet — see [issue #75](https://github.com/cetic-group/terraform-provider-ccp/issues/75).

~> A `for_each` over a value that is unknown at plan time forces a two-step apply when the scale set is created in the same run. Create the scale set first, then the members.
