---
page_title: "ccp_vm_instance Resource - ccp"
subcategory: "Compute"
description: |-
  Manages a virtual machine instance on CETIC Cloud Platform.
---

# ccp_vm_instance (Resource)

Manages a virtual machine instance on CETIC Cloud Platform. VMs are full virtual machines with cloud-init support for initial user, SSH key, and package configuration. They run guest kernels and are suitable for workloads that require kernel-level isolation or specific kernel versions.

~> **Note:** VM creation is asynchronous and includes cloud-init execution on first boot. The provider polls until the VM reaches `running` status. Provisioning typically takes 2 to 4 minutes. Changing `plan` updates the VM in-place (requires a stop/start cycle). Changing `template`, `vnet_id`, or `region` forces a new resource. `disk_gb` grows in place via a resize call — shrinking is rejected.

## Example Usage

```hcl
resource "ccp_ssh_key" "ops" {
  name       = "ops-team"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "ccp_vm_instance" "app" {
  name          = "app-server"
  region        = "RNN"
  plan          = "medium"
  template      = "ubuntu-24.04"
  vnet_id       = ccp_vnet.web.id
  root_password = var.vm_root_password  # sensible — préférer une variable
  ssh_key_ids   = [ccp_ssh_key.ops.id]
  tags          = ["app", "env:prod"]

  user_data = <<-EOF
    #cloud-config
    packages:
      - docker.io
      - docker-compose-v2
    runcmd:
      - systemctl enable --now docker
  EOF
}
```

### Windows VM

Windows VMs use a Windows system image (template key `win-*`) or a custom
template captured from a Windows VM. They are accessed over **RDP** (no SSH);
the administrator account is `Administrator`. CETIC Cloud does **not** provide
Windows licenses — you must hold a valid license per instance and acknowledge
this via `windows_license_consent = true`. A Windows VM requires a plan of
`medium` or larger and a strong administrator password (≥ 12 characters,
covering at least 3 of: lowercase, uppercase, digit, symbol).

```hcl
resource "ccp_vm_instance" "win" {
  name                    = "win-app"
  region                  = "RNN"
  plan                    = "medium" # Windows requires medium or larger
  template                = "win-2022"
  vnet_id                 = ccp_vnet.web.id
  root_password           = var.win_admin_password # Administrator password (strong)
  windows_license_consent = true                   # required for Windows templates
  tags                    = ["windows", "rdp"]
  # No ssh_key_ids / user_data — Windows is RDP-only.
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the VM instance.
- `region` - (Required, Forces new resource) Region where the VM is created. One of: `RNN`, `PAR`, `ABJ`.
- `plan` - (Required) Instance plan controlling vCPU, RAM, and disk. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `template` - (Required, Forces new resource) VM template key (e.g. `ubuntu-24.04`, `debian-12`). Available templates are listed in the console under **Compute → Templates**.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet to attach the VM to.
- `root_password` - (Required, Sensitive, Forces new resource) Root password injected via cloud-init. Length 8–128 chars. Mark the value as sensitive (`sensitive = true` on the variable) and prefer passing it via a TF variable, environment, or secret backend.

### Optional

- `ssh_key_ids` - (Optional) List of SSH key UUIDs to inject via cloud-init. Keys are added to the default cloud user's `authorized_keys`.
- `user_data` - (Optional, Forces new resource) Cloud-init user data. Can be a cloud-config YAML document (`#cloud-config`) or a shell script (`#!/bin/bash`).
- `bastion_access` - (Optional, Forces new resource) Allow SSH access to the VM through the tenant Bastion (opt-in). Defaults to `false`. Requires a Bastion configured for the organization.
- `windows_license_consent` - (Optional, Forces new resource) Acknowledge that CETIC Cloud provides no Windows license (you must hold a valid license per instance). **Required (`true`) when `template` is a Windows system image (`win-*`) or a custom template captured from a Windows VM** — the API rejects the create with HTTP 422 otherwise. Ignored for Linux templates. Windows VMs also require a `medium`+ plan and a strong administrator password (≥ 12 chars, ≥ 3 categories).
- `disk_gb` - (Optional, Computed) Root disk size in GB. Defaults to the selected plan's disk size when omitted. **Mutable in place** — growing this value resizes the disk via the API without recreating the VM. Shrinking is rejected with a diagnostic.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VM instance.
- `status` - Current status. Possible values: `provisioning`, `running`, `stopped`, `error`.
- `os_family` - Operating system family derived from the template: `linux` or `windows`.
- `ip_address` - Private IP address assigned by the VNet IPAM.
- `public_ip_address` - Public IP address if one is currently attached, otherwise empty.

## Import

VM instances can be imported using their UUID:

```
terraform import ccp_vm_instance.app <vm_id>
```
