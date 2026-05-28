---
page_title: "ccp_container_templates Data Source - cetic-cloud-platform"
subcategory: "Catalogs"
description: |-
  Lists active container templates available on CETIC Cloud Platform.
---

# ccp_container_templates (Data Source)

Lists active container templates available on CETIC Cloud Platform's admin-managed catalog. Useful for resolving a template `key` (e.g. `ubuntu-24.04`) to use in the `template` argument of [`ccp_container_instance`](../resources/container_instance.md) or [`ccp_container_scale_set`](../resources/container_scale_set.md).

## Example Usage

```hcl
data "ccp_container_templates" "available" {}

output "default_template" {
  value = [
    for t in data.ccp_container_templates.available.templates :
    t.key if t.is_default
  ][0]
}

resource "ccp_container_instance" "web" {
  name     = "web"
  region   = "RNN"
  plan     = "small"
  vnet_id  = ccp_vnet.web.id
  template = data.ccp_container_templates.available.templates[0].key
  # ...
}
```

## Schema

### Attributes

- `templates` (Attributes List) — List of active container templates.

### Nested Schema for `templates`

- `key` (String) — Template key used in `ccp_container_instance.template`. Example: `ubuntu-24.04`.
- `display_name` (String) — Human-readable template name.
- `is_default` (Bool) — Whether this template is the default suggestion in the console.
