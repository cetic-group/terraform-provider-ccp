---
page_title: "ccp_container_snapshot Resource - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Manages a snapshot of a container instance on CETIC Cloud Platform.
---

# ccp_container_snapshot (Resource)

Creates a point-in-time snapshot of a container instance. Snapshots are stored on Ceph RBD and can be used to restore the container to a previous state via the console or CLI. Up to 2 free snapshots are included per instance.

~> **Note:** Snapshots are immutable after creation. Any change to `container_id` or `name` forces the existing snapshot to be deleted and a new one to be created.

## Example Usage

```hcl
# Snapshot before a major upgrade
resource "ccp_container_snapshot" "pre_upgrade" {
  container_id = ccp_container_instance.web.id
  name         = "before-nginx-upgrade-2026-05"
}

# Weekly baseline snapshot
resource "ccp_container_snapshot" "baseline" {
  container_id = ccp_container_instance.web.id
  name         = "weekly-baseline"
}
```

## Argument Reference

### Required

- `container_id` - (Required, Forces new resource) UUID of the container instance to snapshot.
- `name` - (Required, Forces new resource) Name of the snapshot. Must be unique within the container.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the snapshot.
- `status` - Current status. Possible values: `creating`, `available`, `error`.
- `created_at` - Timestamp when the snapshot was created (RFC3339).

## Import

Container snapshots can be imported using the container UUID and snapshot UUID separated by a slash:

```
terraform import ccp_container_snapshot.pre_upgrade <container_id>/<snapshot_id>
```
