---
page_title: "ccp_lxc_templates Data Source - cetic-cloud-platform"
subcategory: "Catalogs"
description: |-
  Deprecated — use ccp_container_templates instead.
---

# ccp_lxc_templates (Data Source) — Deprecated

~> **Deprecated.** Use [`ccp_container_templates`](container_templates.md) instead. `ccp_lxc_templates` exposes the underlying implementation name (LXC) rather than the canonical metier name (container). The two return the same data and have identical schemas. This alias will be removed in v2.0.0 of the provider.

## Migration

```diff
- data "ccp_lxc_templates" "available" {}
+ data "ccp_container_templates" "available" {}

  resource "ccp_container_instance" "web" {
-   template = data.ccp_lxc_templates.available.templates[0].key
+   template = data.ccp_container_templates.available.templates[0].key
  }
```

Schema is unchanged — refer to the [`ccp_container_templates`](container_templates.md) page for the full reference.
