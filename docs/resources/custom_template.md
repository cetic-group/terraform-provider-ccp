---
page_title: "ccp_custom_template Resource - ccp"
subcategory: "Compute"
description: |-
  Manages a custom template — a reusable snapshot of an existing container or VM.
---

# ccp_custom_template (Resource)

A custom template is a tenant-owned reusable image, created by snapshotting an existing container or VM. Once `ready`, the template can be used as a base image when creating new instances.

Exactly one of `source_container_id` or `source_vm_id` must be set at creation time. Changing the source (or any non-mutable attribute) forces resource replacement.

~> **Note:** Creation is asynchronous on the API side (HTTP 202 Accepted). The resource returns immediately with `status = "creating"`. Poll `ccp_custom_template.status` for `ready` before referencing the template elsewhere.

## Example Usage

### From a container

```hcl
resource "ccp_container_instance" "base" {
  name     = "base-image-builder"
  region   = "RNN"
  plan     = "small"
  template = "ubuntu-24.04"
  vnet_id  = ccp_vnet.web.id

  user_data = <<-EOF
    #!/bin/bash
    apt-get update && apt-get install -y nginx fail2ban
    # Custom hardening, etc.
  EOF
}

# Stop the container, then snapshot it into a reusable template.
resource "ccp_custom_template" "nginx_hardened" {
  name                = "nginx-hardened-v1"
  description         = "Ubuntu 24.04 + nginx + fail2ban baseline"
  source_container_id = ccp_container_instance.base.id
}
```

### From a VM

```hcl
resource "ccp_custom_template" "app_image" {
  name         = "app-base-v3"
  description  = "Base VM image for the application tier"
  source_vm_id = ccp_vm_instance.builder.id
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the template (2-100 chars; alphanumerics, `_`, `-`, spaces).

### Required (mutually exclusive)

Exactly one of the following must be set, and changing it forces replacement:

- `source_container_id` - UUID of the source container to snapshot.
- `source_vm_id` - UUID of the source VM to snapshot.

### Optional

- `description` - Optional description (max 500 chars).

## Attributes Reference

- `id` - UUID of the custom template.
- `template_type` - `container` or `vm` (derived from the source).
- `region` - Region inherited from the source instance.
- `status` - Current status: `creating`, `ready`, `error`, or `deleting`.
- `error_message` - Last error if `status = error`.
- `disk_gb` - Snapshot disk size in gibibytes (set once `ready`).
- `source_instance_id` - Server-side reference (matches `source_container_id` or `source_vm_id`).
- `source_instance_type` - `container` or `vm`.
- `created_at` - Creation timestamp (ISO-8601).
- `updated_at` - Last update timestamp (ISO-8601).

## Import

Custom templates can be imported using their UUID:

```
terraform import ccp_custom_template.nginx_hardened <uuid>
```
