---
page_title: "ccp_registry Data Source - ccp"
subcategory: "Registry"
description: |-
  Look up an existing CETIC Container Registry by ID or by (name, region).
---

# ccp_registry (Data Source)

Look up an existing CETIC Container Registry by `id`, or by the unique combination `(name, region)`.

## Example Usage

```hcl
# By ID
data "ccp_registry" "ccr_prod" {
  id = "11111111-2222-3333-4444-555555555555"
}

# By name + region
data "ccp_registry" "ccr_prod_alt" {
  name   = "prod"
  region = "RNN"
}

output "registry_url" {
  value = data.ccp_registry.ccr_prod.url
}
```

## Argument Reference

Provide **either** `id`, **or** the pair `(name, region)`. Combining the two yields an error.

### Optional

- `id` — UUID of the registry to look up.
- `name` — Name of the registry. Combine with `region`.
- `region` — Region of the registry. Combine with `name`.

## Attributes Reference

- `id` — UUID of the registry.
- `name` — Human-readable name.
- `slug` — URL-safe slug.
- `region` — Region code.
- `expose_public` — Whether the registry is reachable from the public Internet.
- `expose_private` — Whether the registry is reachable from the CETIC private LAN.
- `url` — Full HTTPS URL (`https://<slug>-<id8>.registry-<region>.cloud.cetic-group.com`). Same value whether reached via Internet or LAN — DNS split-horizon picks the right IP.
- `image_tag` — Tag of the upstream `registry` image currently deployed.
- `gc_schedule_cron` — 5-field cron expression for the weekly GC job.
- `status` — `creating`, `provisioning`, `active`, `error`, or `deleting`.
- `storage_used_gb` — Approximate storage used by registry blobs (gigabytes).
- `last_push_at` — RFC 3339 timestamp of the last push observed.
- `admin_username` — Username of the auto-provisioned admin user.
- `tags` — Free-form tags.
- `created_at` — RFC 3339 creation timestamp.

~> **Note:** The data source does not expose `admin_password` — that secret is only available at the creation time of [`ccp_registry`](../resources/registry.md).
