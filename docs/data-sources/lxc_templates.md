---
page_title: "ccp_lxc_templates Data Source - cetic-cloud-platform"
subcategory: "Catalogs"
description: |-
  Lists active container templates available on CETIC Cloud.
---

# ccp_lxc_templates (Data Source)

Lists active container templates from the CETIC Cloud catalog (admin-managed). Use this to resolve a template `key` (e.g. `ubuntu-24.04`) at plan-time instead of hardcoding it in `ccp_container_instance.template`.

## Example Usage

```hcl
data "ccp_lxc_templates" "all" {}

output "available_lxc_templates" {
  value = [for t in data.ccp_lxc_templates.all.templates : t.key]
}

# Pick the default template programmatically
locals {
  default_lxc = one([for t in data.ccp_lxc_templates.all.templates : t if t.is_default])
}

resource "ccp_container_instance" "web" {
  name     = "web-01"
  region   = "RNN"
  plan     = "small"
  template = local.default_lxc.key
  vnet_id  = ccp_vnet.web.id
}
```

## Attributes Reference

- `templates` - List of active container templates.
  - `key` - Template key (used in `ccp_container_instance.template`).
  - `display_name` - Human-readable template name.
  - `is_default` - Whether this template is the default suggestion in the console.
