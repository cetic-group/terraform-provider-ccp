---
page_title: "ccp_container_instance Resource - ccp"
subcategory: "Compute"
description: |-
  Manages a container instance on CETIC Cloud Platform.
---

# ccp_container_instance (Resource)

Manages a container instance on CETIC Cloud Platform. Containers are high-performance, lightweight compute units that boot in seconds, share the host kernel, and support cloud-init for first-boot configuration.

~> **Note:** Container creation is asynchronous. The provider polls until the container reaches `running` status. Provisioning typically completes within 60 seconds. Changing `plan` updates the container in-place (requires a stop/start cycle). Changing `template`, `vnet_id`, or `region` forces a new resource.

## Example Usage

```hcl
resource "ccp_ssh_key" "ops" {
  name       = "ops-team"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "ccp_container_instance" "web" {
  name          = "web-01"
  region        = "RNN"
  plan          = "small"
  template      = "ubuntu-24.04"
  vnet_id       = ccp_vnet.web.id
  root_password = var.container_root_password  # sensible — préférer une variable
  ssh_key_ids   = [ccp_ssh_key.ops.id]
  tags          = ["web", "env:prod"]

  user_data = <<-EOF
    #!/bin/bash
    apt-get update -q && apt-get install -y -q nginx
    systemctl enable --now nginx
    echo "healthy" > /var/www/html/healthz
  EOF
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the container instance.
- `region` - (Required, Forces new resource) Region where the container is created. One of: `RNN`, `PAR`, `ABJ`.
- `plan` - (Required) Instance plan controlling CPU, RAM, and disk. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `template` - (Required, Forces new resource) Template key for the container OS image (e.g. `ubuntu-24.04`, `debian-12`). Available templates are listed in the console under **Compute → Templates**.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet to attach the container to.
- `root_password` - (Required, Sensitive, Forces new resource) Root password injected at first boot. Length 8–128 chars. Mark the value as sensitive (`sensitive = true` on the variable) and prefer passing it via a TF variable, environment, or secret backend.

### Optional

- `ssh_key_ids` - (Optional) List of SSH key UUIDs to inject at creation time. Keys are added to `/root/.ssh/authorized_keys` inside the container.
- `user_data` - (Optional, Forces new resource) Cloud-init script executed on first boot. Can be a shell script (`#!/bin/bash`) or cloud-config YAML (`#cloud-config`).
- `bastion_access` - (Optional, Forces new resource) Allow SSH access to the container through the tenant Bastion (opt-in). Defaults to `false`. Requires a Bastion configured for the organization.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the container instance.
- `status` - Current status. Possible values: `provisioning`, `running`, `stopped`, `error`.
- `ip_address` - Private IP address assigned by the VNet IPAM.
- `public_ip_address` - Public IP address if one is currently attached, otherwise empty.

## Import

Container instances can be imported using their UUID:

```
terraform import ccp_container_instance.web <container_id>
```
