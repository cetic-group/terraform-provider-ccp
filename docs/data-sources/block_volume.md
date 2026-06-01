---
page_title: "ccp_block_volume Data Source - ccp"
subcategory: "Storage"
description: |-
  Look up a Block Volume (Ceph RBD) by ID or (name, region).
---

# ccp_block_volume (Data Source)

Look up a Block Volume (Ceph RBD) by `id` or `(name, region)`.

## Example Usage

```hcl
data "ccp_block_volume" "data" {
  name   = "data-disk"
  region = "RNN"
}
```

## Attributes Reference

- `id`, `name`, `region`, `size_gb`, `status`
- `attached_to_id`, `attached_to_type`, `attached_to_name` (nullable)
- `rbd_pool`, `rbd_image`, `error_message` (nullable)
- `tags`, `created_at`, `updated_at`
