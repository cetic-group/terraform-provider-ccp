---
page_title: "ccp_windows_instance Resource - ccp"
subcategory: "Compute"
description: |-
  Manages a Windows instance on CETIC Cloud Platform.
---

# ccp_windows_instance (Resource)

Manages a Windows instance on CETIC Cloud Platform. Windows instances are full virtual machines provisioned via the dockur stack.

~> **License notice:** CETIC Cloud Platform provides **no** Windows license. You are responsible for holding a valid Windows license for each instance you create. You **must** set `license_consent = true` to acknowledge this — the provider rejects the plan otherwise.

~> **Note:** The Windows API has no in-place update endpoint, so **every** user-settable argument forces a new resource when changed. Creation is asynchronous: the provider polls until the instance reaches `running` status. Windows installs are slow — provisioning typically takes 10 to 20 minutes.

## Example Usage

```hcl
resource "ccp_windows_instance" "win" {
  name                   = "win-app"
  region                 = "RNN"
  plan                   = "large"
  template               = "windows-2022"
  vnet_id                = ccp_vnet.web.id
  administrator_password = var.windows_admin_password # sensitive — prefer a variable
  data_volume_ids        = [ccp_block_volume.data.id]
  tags                   = ["windows", "env:prod"]

  # Required acknowledgement: CETIC Cloud provides no Windows license.
  license_consent = true
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Name of the Windows instance (1–100 chars; letters, digits, `_`, `-`).
- `region` - (Required, Forces new resource) Region where the instance is created. One of: `RNN`, `PAR`, `ABJ`.
- `template` - (Required, Forces new resource) Windows template key (e.g. `windows-2022`, `windows-11`). Available templates are listed in the console under **Compute → Templates**.
- `plan` - (Required, Forces new resource) Instance plan controlling vCPU, RAM, and disk. One of: `nano`, `micro`, `small`, `medium`, `large`, `xlarge`.
- `administrator_password` - (Required, Sensitive, Forces new resource) Administrator password set during install. Length 8–128 chars. Mark the value as sensitive and prefer passing it via a TF variable, environment, or secret backend.
- `license_consent` - (Required, Forces new resource) Must be `true`. CETIC Cloud provides no Windows license; you are responsible for holding a valid license for each instance.

### Optional

- `vnet_id` - (Optional, Forces new resource) UUID of the VNet to attach the instance to. If omitted, the instance is created in the tenant's default network.
- `public_ip_id` - (Optional, Forces new resource) UUID of a public IP to attach at creation. The Windows API has no live attach/detach for instances.
- `data_volume_ids` - (Optional, Forces new resource) List of block volume UUIDs to attach (max 5).
- `tags` - (Optional, Forces new resource) List of free-form tags.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the Windows instance.
- `hostname` - Computed hostname of the instance.
- `cores` - vCPU count derived from the selected plan.
- `memory_mb` - Memory in MB derived from the selected plan.
- `disk_gb` - Root disk size in GB derived from the selected plan.
- `status` - Current status. Possible values: `installing`, `provisioning`, `running`, `stopped`, `error`, `deleting`.
- `ip_address` - Private IP address assigned by the VNet IPAM.
- `public_ip_address` - Public IP address if one is currently attached, otherwise empty.
- `has_admin_password` - Whether an administrator password is set on the instance.
- `error_message` - Last error message reported by the provisioner, if any.
- `created_at` - RFC 3339 timestamp at which the instance was created.
- `updated_at` - RFC 3339 timestamp of the last server-side update.

## Import

Windows instances can be imported using their UUID:

```
terraform import ccp_windows_instance.win <windows_instance_id>
```
