---
page_title: "ccp_registry Resource - ccp"
subcategory: "Registry"
description: |-
  Manages a CETIC Container Registry (CCR) — a private OCI artifact registry hosted in your tenant.
---

# ccp_registry (Resource)

Manages a CETIC Container Registry (CCR) — a Distribution-based, OCI-compliant
container/Helm/OCI artifact registry hosted in a per-tenant LXC container.
Each registry exposes a hostname under `<slug>.cloud.cetic-group.com` (custom
domains are backlogged) and is reachable via standard `docker login` /
`docker push` / `docker pull` workflows.

~> **Note:** Registry provisioning is asynchronous. The provider polls until
the registry reaches `active` status, which typically takes 5 to 10 minutes
(TLS issuance plus, for `private` exposure, DNS-01 propagation through IONOS).

~> **Important — `admin_password`:** The `admin_password` attribute is
returned **only at creation** by the API. It is written to the Terraform
state and cannot be retrieved afterwards. Treat your state file as
sensitive (encrypted backend, restricted IAM). To rotate the password,
`terraform taint` the resource: the destroy + create cycle issues a fresh
password.

~> **Workload identity:** Pods running in CETIC Cloud Kubernetes (CCKS)
authenticate to their tenant registry transparently via SA token exchange,
managed by the cluster-agent. You do **not** need to provision an
`imagePullSecret` for in-cluster workloads.

## Example Usage

### Private registry (DNS-01 IONOS, reachable from peer VNets only)

```hcl
resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
}

resource "ccp_vnet" "registry" {
  vpc_id = ccp_vpc.main.id
  name   = "registry-tier"
  cidr   = "10.0.10.0/24"
}

resource "ccp_registry" "private" {
  name             = "ccr-prod"
  region           = "RNN"
  vpc_id           = ccp_vpc.main.id
  vnet_id          = ccp_vnet.registry.id
  exposure         = "private"
  gc_schedule_cron = "0 3 * * 0" # Sunday 03:00 UTC
  tags             = ["env:prod"]
}

output "registry_hostname" {
  value = ccp_registry.private.hostname
}

output "registry_admin_password" {
  value     = ccp_registry.private.admin_password
  sensitive = true
}
```

### Public registry (HTTP-01 Let's Encrypt, reachable from the internet)

```hcl
resource "ccp_public_ip" "registry" {
  region = "RNN"
}

resource "ccp_registry" "public" {
  name         = "ccr-public"
  region       = "RNN"
  vpc_id       = ccp_vpc.main.id
  vnet_id      = ccp_vnet.registry.id
  exposure     = "public"
  public_ip_id = ccp_public_ip.registry.id
  tags         = ["env:prod"]
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name (1-100 chars).
- `region` - (Required, Forces new resource) Region code. One of: `RNN`, `PAR`, `ABJ`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC the registry runs in.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet the registry runs in.
- `exposure` - (Required, Forces new resource) `public` or `private`.
  - `public` issues a Let's Encrypt cert via HTTP-01 and exposes the registry
    on the internet (a `public_ip_id` should also be set).
  - `private` issues via DNS-01 IONOS — the registry is only reachable from
    peer VNets / VPN clients.

### Optional

- `public_ip_id` - (Optional) UUID of a `ccp_public_ip` to attach as the public
  entrypoint. Required when `exposure = "public"`. Set/unset to attach/detach
  on subsequent applies.
- `image_tag` - (Optional, Computed) Tag of the upstream `registry` image to
  deploy. Defaults to the platform-managed default (currently `2.8`). Pin to
  opt out of platform-driven bumps.
- `gc_schedule_cron` - (Optional, Computed) 5-field cron expression for the
  weekly garbage-collection job. Defaults to `0 3 * * 0` (Sunday 03:00 UTC).
  GC pauses pushes for ~30-60s.
- `tags` - (Optional, Computed) Free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the registry.
- `slug` - URL-safe slug derived from `name`, used as the hostname prefix.
- `hostname` - Fully qualified hostname (e.g. `ccr-prod.cloud.cetic-group.com`).
- `public_ip` - Public IPv4 address currently routing traffic, if any.
- `status` - Provisioning status: `provisioning`, `active`, `updating`, `error`, `deleting`.
- `storage_used_mb` - Approximate storage used by registry blobs (megabytes), refreshed periodically.
- `last_activity_at` - RFC 3339 timestamp of the last push/pull observed by the proxy.
- `admin_username` - Username of the auto-provisioned admin user (typically `admin`).
- `admin_password` - (Sensitive) Password of the admin user, returned **only at creation**. Stored in the state.
- `created_at` - RFC 3339 creation timestamp.

## Import

Registries can be imported using their UUID:

```
terraform import ccp_registry.private <registry_id>
```

~> **Note:** After import, `admin_password` is unset in state — it cannot be
retrieved from the API. To recover credentials, taint the resource (which
re-creates it and issues a new password).
