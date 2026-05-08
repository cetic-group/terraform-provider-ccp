---
page_title: "ccp_qemu_templates Data Source - cetic-cloud-platform"
subcategory: "Catalogs"
description: |-
  Lists active QEMU/VM templates available on CETIC Cloud.
---

# ccp_qemu_templates (Data Source)

Lists active QEMU/VM templates from the CETIC Cloud catalog (admin-managed). Internal `ccks-*` images for managed Kubernetes nodes are excluded — only client-usable templates are returned.

## Example Usage

```hcl
data "ccp_qemu_templates" "all" {}

# Pick the default
locals {
  default_vm_tpl = one([for t in data.ccp_qemu_templates.all.templates : t if t.is_default])
}

resource "ccp_vm_instance" "app" {
  name     = "app-01"
  region   = "RNN"
  plan     = "medium"
  template = local.default_vm_tpl.key
  vnet_id  = ccp_vnet.web.id
}
```

## Attributes Reference

- `templates` - List of active VM templates suitable for client VMs / VM scale sets.
  - `key` - Template key (used in `ccp_vm_instance.template`).
  - `display_name` - Human-readable template name.
  - `is_default` - Whether this template is the default suggestion in the console.
