---
page_title: "ccp_vm_instance Resource - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Manages a virtual machine instance on CETIC Cloud Platform.
---

# ccp_vm_instance (Resource)

Manages a virtual machine instance on CETIC Cloud Platform. VMs are full virtual machines with cloud-init support for initial user, SSH key, and package configuration. They run guest kernels and are suitable for workloads that require kernel-level isolation or specific kernel versions.

~> **Note:** VM creation is asynchronous and includes cloud-init execution on first boot. The provider polls until the VM reaches `running` status. Provisioning typically takes 2 to 4 minutes. Changing `plan` updates the VM in-place (requires a stop/start cycle). Changing `template`, `vnet_id`, or `region` forces a new resource.

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
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VM instance.
- `status` - Current status. Possible values: `provisioning`, `running`, `stopped`, `error`.
- `ip_address` - Private IP address assigned by the VNet IPAM.
- `public_ip_address` - Public IP address if one is currently attached, otherwise empty.

## Import

VM instances can be imported using their UUID:

```
terraform import ccp_vm_instance.app <vm_id>
```
