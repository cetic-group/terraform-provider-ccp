---
page_title: "ccp_vm_scale_set Resource - ccp"
subcategory: "Compute"
description: |-
  Manages a VM scale set (auto-scaling group of virtual machines) on CETIC Cloud Platform.
---

# ccp_vm_scale_set (Resource)

Manages a VM scale set — a group of identical virtual machine instances that scale horizontally. The platform monitors instance health and automatically replaces failed VMs (auto-repair). Setting `min_instances` and `max_instances` enables auto-scaling.

~> **Note:** Scale set creation is asynchronous. The provider polls until the scale set reaches `active` status. Changing `desired_instances` / `min_instances` / `max_instances` scales the set in place. Adding or removing a VNet (primary or secondary) is applied **without recreating** the members. Changing `template`, `vnet_id`, or `region` forces a new resource.

## Example Usage

```hcl
resource "ccp_vm_scale_set" "compute_workers" {
  name              = "compute-workers"
  region            = "RNN"
  plan              = "large"
  template          = "ubuntu-24.04"
  vnet_id           = ccp_vnet.web.id
  desired_instances = 2
  min_instances     = 1
  max_instances     = 8
  root_password     = var.vmss_root_password # sensible — préférer une variable
  tags              = ["workers", "env:prod"]
}
```

### Windows VM scale set

Windows scale sets use a Windows system image (template key `win-*`) or a custom
template captured from a Windows VM. Members are accessed over **RDP** (no SSH);
the administrator account is `Administrator`. CETIC Cloud does **not** provide
Windows licenses — you must hold a valid license per member instance and
acknowledge this via `windows_license_consent = true`. Windows scale sets
require a plan of `medium` or larger and a strong administrator password
(≥ 12 characters, covering at least 3 of: lowercase, uppercase, digit, symbol).

```hcl
resource "ccp_vm_scale_set" "win_workers" {
  name                    = "win-workers"
  region                  = "RNN"
  plan                    = "medium" # Windows requires medium or larger
  template                = "win-2022"
  vnet_id                 = ccp_vnet.web.id
  desired_instances       = 2
  min_instances           = 1
  max_instances           = 4
  root_password           = var.win_admin_password # Administrator password (strong)
  windows_license_consent = true                   # required for Windows templates
  tags                    = ["windows", "rdp"]
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the scale set.
- `region` - (Required, Forces new resource) Region where the scale set is created. One of: `RNN`, `PAR`, `ABJ`.
- `plan` - (Required) Instance plan for each VM. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `template` - (Required, Forces new resource) VM template key (e.g. `ubuntu-24.04`, `debian-12`, `win-2022`).
- `vnet_id` - (Required, Forces new resource) UUID of the VNet to attach all VMs to.
- `desired_instances` - (Required) Desired number of VM replicas.
- `root_password` - (Required, Sensitive, Forces new resource) Root (or Windows administrator) password injected on every VM of the scale set. Length 8–128 chars (Windows requires ≥ 12 + 3 categories). Mark the value as sensitive and prefer passing it via a TF variable, environment, or secret backend.

### Optional

- `min_instances` - (Optional) Minimum number of replicas for auto-scaling. Must be greater than or equal to 0. Defaults to `1`.
- `max_instances` - (Optional) Maximum number of replicas for auto-scaling. Must be greater than or equal to `desired_instances`. Defaults to `10`.
- `auto_repair` - (Optional) Recreate failed/stopped members automatically. Defaults to `true`.
- `bastion_access` - (Optional, Forces new resource) Allow SSH access to every replica through the tenant Bastion (opt-in, Linux only). Defaults to `false`. Requires a Bastion configured for the organization.
- `windows_license_consent` - (Optional, Forces new resource) Acknowledge that CETIC Cloud provides no Windows license (you must hold a valid license per member instance). **Required (`true`) when `template` is a Windows system image (`win-*`) or a custom template captured from a Windows VM** — the API rejects the create with HTTP 422 otherwise. Ignored for Linux templates. Windows scale sets also require a `medium`+ plan and a strong administrator password (≥ 12 chars, ≥ 3 categories).
- `disk_gb` - (Optional, Computed, Forces new resource) Root disk size in GB applied to every replica. Defaults to the selected plan's disk size when omitted. No resize endpoint exists for scale sets, so changing this value forces replacement of the whole scale set.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the scale set.
- `status` - Current status. Possible values: `provisioning`, `active`, `scaling`, `error`.
- `os_family` - Operating system family derived from the template: `linux` or `windows`.

## Import

VM scale sets can be imported using their UUID:

```
terraform import ccp_vm_scale_set.compute_workers <scale_set_id>
```
