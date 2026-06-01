---
page_title: "ccp_vm_snapshot Resource - ccp"
subcategory: "Compute"
description: |-
  Manages a snapshot of a VM instance on CETIC Cloud Platform.
---

# ccp_vm_snapshot (Resource)

Creates a point-in-time snapshot of a VM instance. Snapshots capture the full disk state and can be used to restore the VM to a known-good state via the console or CLI. Up to 2 free snapshots are included per instance.

~> **Note:** Snapshots are immutable after creation. Any change to `vm_instance_id` or `name` forces the existing snapshot to be deleted and a new one to be created.

## Example Usage

```hcl
# Snapshot before applying a kernel upgrade
resource "ccp_vm_snapshot" "pre_kernel_upgrade" {
  vm_instance_id = ccp_vm_instance.app.id
  name           = "before-kernel-6.8-upgrade"
}
```

## Argument Reference

### Required

- `vm_instance_id` - (Required, Forces new resource) UUID of the VM instance to snapshot.
- `name` - (Required, Forces new resource) Name of the snapshot. Must be unique within the VM instance.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the snapshot.
- `status` - Current status. Possible values: `creating`, `available`, `error`.
- `created_at` - Timestamp when the snapshot was created (RFC3339).

## Import

VM snapshots can be imported using the VM UUID and snapshot UUID separated by a slash:

```
terraform import ccp_vm_snapshot.pre_kernel_upgrade <vm_instance_id>/<snapshot_id>
```
