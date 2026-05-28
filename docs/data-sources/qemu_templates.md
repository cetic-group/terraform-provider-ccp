---
page_title: "ccp_qemu_templates Data Source - cetic-cloud-platform"
subcategory: "Catalogs"
description: |-
  Deprecated — use ccp_vm_templates instead.
---

# ccp_qemu_templates (Data Source) — Deprecated

~> **Deprecated.** Use [`ccp_vm_templates`](vm_templates.md) instead. `ccp_qemu_templates` exposes the underlying implementation name (QEMU) rather than the canonical metier name (VM). The two return the same data and have identical schemas. This alias will be removed in v2.0.0 of the provider.

## Migration

```diff
- data "ccp_qemu_templates" "available" {}
+ data "ccp_vm_templates" "available" {}

  resource "ccp_vm_instance" "web" {
-   template = data.ccp_qemu_templates.available.templates[0].key
+   template = data.ccp_vm_templates.available.templates[0].key
  }
```

Schema is unchanged — refer to the [`ccp_vm_templates`](vm_templates.md) page for the full reference.
