---
page_title: "ccp_container_scale_set Resource - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Manages a container scale set (auto-scaling group of LXC containers) on CETIC Cloud Platform.
---

# ccp_container_scale_set (Resource)

Manages a container scale set — a group of identical container instances that scale horizontally. The platform monitors health and automatically replaces failed instances (auto-repair). Setting `min_replicas` and `max_replicas` enables auto-scaling based on CPU utilisation.

~> **Note:** Scale set creation is asynchronous. The provider polls until the scale set reaches `active` status. Changing `replicas` scales the set in place. Changing `template`, `vnet_id`, or `region` forces a new resource.

## Example Usage

```hcl
resource "ccp_container_scale_set" "api_workers" {
  name         = "api-workers"
  region       = "RNN"
  plan         = "small"
  template     = "ubuntu-24.04"
  vnet_id      = ccp_vnet.web.id
  replicas     = 3
  min_replicas = 2
  max_replicas = 10
  tags         = ["api", "env:prod"]
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the scale set.
- `region` - (Required, Forces new resource) Region where the scale set is created. One of: `RNN`, `PAR`, `ABJ`.
- `plan` - (Required) Instance plan for each container. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `template` - (Required, Forces new resource) Template key for the container OS image (e.g. `ubuntu-24.04`).
- `vnet_id` - (Required, Forces new resource) UUID of the VNet to attach all containers to.
- `replicas` - (Required) Desired number of container replicas.

### Optional

- `min_replicas` - (Optional) Minimum number of replicas for auto-scaling. Must be greater than or equal to 1. Defaults to `1`.
- `max_replicas` - (Optional) Maximum number of replicas for auto-scaling. Must be greater than or equal to `replicas`. Defaults to `10`.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the scale set.
- `status` - Current status. Possible values: `provisioning`, `active`, `scaling`, `error`.
- `current_replicas` - Current number of running replicas.

## Import

Container scale sets can be imported using their UUID:

```
terraform import ccp_container_scale_set.api_workers <scale_set_id>
```
