---
page_title: "ccp_block_volume Resource - cetic-cloud-platform"
subcategory: "Storage"
description: |-
  Manages a block storage volume on CETIC Cloud Platform.
---

# ccp_block_volume (Resource)

Manages a block storage volume on CETIC Cloud Platform. Volumes can be attached to container instances or VM instances and appear as raw block devices inside the guest (e.g. `/dev/sdb`). Volumes persist independently of the instances they are attached to — detaching and reattaching to a different instance is supported.

~> **Note:** A volume can be attached to at most one instance at a time. Detaching and re-attaching is supported. Resizing (`size_gb`) is supported in-place when the volume is attached — the guest filesystem must be grown separately after resize.

## Example Usage

```hcl
# Standalone data volume for a PostgreSQL container
resource "ccp_block_volume" "pg_data" {
  name    = "postgres-data"
  region  = "RNN"
  size_gb = 200
  tags    = ["database", "env:prod"]
}

# Volume attached to a container at creation
resource "ccp_block_volume" "app_data" {
  name             = "app-data"
  region           = "RNN"
  size_gb          = 50
  attached_to_id   = ccp_container_instance.web.id
  attached_to_type = "container"
  tags             = ["app"]
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the volume.
- `region` - (Required, Forces new resource) Region where the volume is created. One of: `RNN`, `PAR`, `ABJ`.
- `size_gb` - (Required) Size of the volume in GB. Minimum: `1`. Can be increased in-place; decreasing is not supported.

### Optional

- `attached_to_id` - (Optional) UUID of the instance to attach this volume to. When set, `attached_to_type` must also be specified.
- `attached_to_type` - (Optional) Type of instance to attach to. One of: `container`, `vm`.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the block volume.
- `status` - Current status. Possible values: `available`, `attaching`, `attached`, `detaching`, `error`.

## Import

Block volumes can be imported using their UUID:

```
terraform import ccp_block_volume.pg_data <volume_id>
```
