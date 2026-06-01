---
page_title: "ccp_vm_templates Data Source - ccp"
subcategory: "Catalogs"
description: |-
  Lists active VM templates available on CETIC Cloud Platform.
---

# ccp_vm_templates (Data Source)

Lists active VM templates available on CETIC Cloud Platform's admin-managed catalog (excludes internal `ccks-*` Kubernetes images). Useful for resolving a template `key` (e.g. `ubuntu-24.04-cloud`) to use in the `template` argument of [`ccp_vm_instance`](../resources/vm_instance.md) or [`ccp_vm_scale_set`](../resources/vm_scale_set.md).

## Example Usage

```hcl
data "ccp_vm_templates" "available" {}

output "default_template" {
  value = [
    for t in data.ccp_vm_templates.available.templates :
    t.key if t.is_default
  ][0]
}

resource "ccp_vm_instance" "web" {
  name          = "web"
  region        = "RNN"
  plan          = "medium"
  vnet_id       = ccp_vnet.web.id
  template      = data.ccp_vm_templates.available.templates[0].key
  root_password = var.vm_root_password
  # ...
}
```

## Schema

### Attributes

- `templates` (Attributes List) — List of active VM templates suitable for client VMs / VM scale sets.

### Nested Schema for `templates`

- `key` (String) — Template key used in `ccp_vm_instance.template`. Example: `ubuntu-24.04-cloud`.
- `display_name` (String) — Human-readable template name.
- `is_default` (Bool) — Whether this template is the default suggestion in the console.
