---
page_title: "ccp_vm_scale_set Resource - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Manages a VM scale set (auto-scaling group of QEMU virtual machines) on CETIC Cloud Platform.
---

# ccp_vm_scale_set (Resource)

Manages a VM scale set — a group of identical virtual machine instances that scale horizontally. The platform monitors instance health and automatically replaces failed VMs (auto-repair). Setting `min_replicas` and `max_replicas` enables auto-scaling.

~> **Note:** Scale set creation is asynchronous. The provider polls until the scale set reaches `active` status. Changing `replicas` scales the set in place. Changing `template`, `vnet_id`, or `region` forces a new resource (rolling replacement of all VMs).

## Example Usage

```hcl
resource "ccp_vm_scale_set" "compute_workers" {
  name         = "compute-workers"
  region       = "RNN"
  plan         = "large"
  template     = "ubuntu-24.04"
  vnet_id      = ccp_vnet.web.id
  replicas     = 2
  min_replicas = 1
  max_replicas = 8
  tags         = ["workers", "env:prod"]
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the scale set.
- `region` - (Required, Forces new resource) Region where the scale set is created. One of: `RNN`, `PAR`, `ABJ`.
- `plan` - (Required) Instance plan for each VM. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `template` - (Required, Forces new resource) QEMU template key (e.g. `ubuntu-24.04`, `debian-12`).
- `vnet_id` - (Required, Forces new resource) UUID of the VNet to attach all VMs to.
- `replicas` - (Required) Desired number of VM replicas.

### Optional

- `min_replicas` - (Optional) Minimum number of replicas for auto-scaling. Must be greater than or equal to 1. Defaults to `1`.
- `max_replicas` - (Optional) Maximum number of replicas for auto-scaling. Must be greater than or equal to `replicas`. Defaults to `10`.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the scale set.
- `status` - Current status. Possible values: `provisioning`, `active`, `scaling`, `error`.
- `current_replicas` - Current number of running VM replicas.

## Import

VM scale sets can be imported using their UUID:

```
terraform import ccp_vm_scale_set.compute_workers <scale_set_id>
```
