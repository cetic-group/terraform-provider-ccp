---
page_title: "ccp_application_gateway Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a CETIC Cloud Application Gateway (L7 HTTP/HTTPS reverse proxy with TLS, rate limiting and WAF).
---

# ccp_application_gateway (Resource)

Manages a CETIC Cloud **Application Gateway** (`ccp-appgw`) — a fully managed L7 HTTP/HTTPS reverse proxy that terminates TLS, multiplexes hostnames via SNI, applies per-route policies (rate limit, IP allow/deny, CORS, basic auth, WAF) and load-balances to a pool of backends.

Each gateway is highly available with a floating virtual IP and automatic failover. Listeners, routes and target groups are declared as separate resources to keep per-route HCL edits idempotent:

- [`ccp_appgw_listener`](appgw_listener.md) — one hostname (and Let's Encrypt cert) per listener
- [`ccp_appgw_target_group`](appgw_target_group.md) — a pool of backends with algorithm + health check
- [`ccp_appgw_target_group_member`](appgw_target_group_member.md) — one backend (container / VM / raw IP) inside a target group
- [`ccp_appgw_route`](appgw_route.md) — one routing rule (path + headers + methods → target group) with L7 policies

~> **Provisioning is asynchronous.** The provider polls until status is `active` (typically 3-5 minutes for the initial create).

## Example Usage

```hcl
resource "ccp_public_ip" "appgw" {
  region = "RNN"
}

resource "ccp_application_gateway" "web" {
  name         = "web-appgw"
  region       = "RNN"
  plan         = "appgw-medium"
  vpc_id       = ccp_vpc.main.id
  vnet_id      = ccp_vnet.web.id
  public_ip_id = ccp_public_ip.appgw.id

  force_https            = true
  hsts_enabled           = true
  hsts_max_age           = 31536000
  global_rate_limit_per_sec = 1000
  global_allow_cidrs     = []
  global_deny_cidrs      = ["198.51.100.0/24"]
  trust_proxy_headers    = false

  tags = ["env:prod", "team:web"]
}

resource "ccp_appgw_listener" "api" {
  appgw_id = ccp_application_gateway.web.id
  hostname = "api.example.com"
}

resource "ccp_appgw_target_group" "api_pool" {
  appgw_id   = ccp_application_gateway.web.id
  name       = "api-pool"
  algorithm  = "leastconn"
  hc_path    = "/healthz"
}

resource "ccp_appgw_target_group_member" "api_01" {
  appgw_id        = ccp_application_gateway.web.id
  target_group_id = ccp_appgw_target_group.api_pool.id
  container_id    = ccp_container_instance.api_01.id
  port            = 8080
}

resource "ccp_appgw_route" "api_v1" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.api.id
  priority        = 10
  path_match      = "/v1/"
  path_match_type = "prefix"
  target_group_id = ccp_appgw_target_group.api_pool.id
}
```

### Pinned private address

Reserve the address, then hand it to the gateway. Keep it outside the address
block of any Kubernetes cluster sharing the VNet.

```hcl
resource "ccp_vnet_ip_reservation" "web_vip" {
  vnet_id     = ccp_vnet.web.id
  name        = "web-vip"
  ip          = "10.0.1.200"
  description = "Private address of the gateway"
}

resource "ccp_application_gateway" "web" {
  name        = "web-appgw"
  region      = "RNN"
  plan        = "appgw-medium"
  vpc_id      = ccp_vpc.main.id
  vnet_id     = ccp_vnet.web.id
  vip_address = ccp_vnet_ip_reservation.web_vip.ip
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name of the gateway (1-100 chars).
- `region` - (Required, Forces new resource) Region code (`RNN`, `PAR`, `ABJ`).
- `plan` - (Required) Capacity plan key, e.g. `appgw-small`, `appgw-medium`, `appgw-large`. Validated server-side against the live plan catalog — backoffice-defined plans are accepted without a provider upgrade.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC the gateway is provisioned in.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet the gateway VIP is hosted on. Backends declared via target group members must be reachable from this VNet.

### Optional

- `cache_enabled` (Boolean, Optional) — Enables the gateway HTTP cache. Default `false`; also enable caching on individual routes.

- `public_ip_id` - (Optional) UUID of a `ccp_public_ip` to attach as the public entrypoint. Set to attach, remove to detach.
- `vip_address` - (Optional, Forces new resource) Private IPv4 address of the gateway in the VNet, in canonical dotted form (`10.0.1.200`). When omitted, the platform picks a free address and exports it. Set it to pin the address: reserve it first with `ccp_vnet_ip_reservation` (a reservation of your own organization is accepted). The API rejects an address outside the VNet or below the start of the network's address range, an address reserved for the network itself (network, gateway `.1`, broadcast), or one already in use — including the address block reserved for the nodes of a Kubernetes cluster on the same VNet, and an address reserved by another organization on a shared network.
- `force_https` - (Optional, default `true`) Redirect plain HTTP (`:80`) traffic to HTTPS (`:443`).
- `hsts_enabled` - (Optional, default `false`) Send the `Strict-Transport-Security` header on every HTTPS response.
- `hsts_max_age` - (Optional, default `31536000`) `max-age` directive of the HSTS header in seconds.
- `global_rate_limit_per_sec` - (Optional) Gateway-wide rate limit (req/sec/IP). Null disables the global limit — routes can still set their own.
- `global_allow_cidrs` - (Optional) List of CIDRs allowed to hit any route. Empty = allow all.
- `global_deny_cidrs` - (Optional) List of CIDRs denied access — evaluated before allow.
- `trust_proxy_headers` - (Optional, default `false`) Accept incoming `X-Forwarded-For` / `X-Real-IP` headers from clients.
- `tags` - (Optional) Free-form tags (max 60, max 50 chars each).
- `custom_error_page_html` - (Optional) Your own HTML page, served instead of the default one when no backend can answer (HTTP 502, 503 and 504). At most 7 KiB (UTF-8 bytes) once leading and trailing whitespace is trimmed — inline the styles and host images elsewhere. Updated in place; **removing the attribute restores the default page**. Leading/trailing whitespace (e.g. the final newline of `file()`) does not cause a diff.

  ```hcl
  custom_error_page_html = file("${path.module}/maintenance.html")
  ```

## Attributes Reference

- `cache_size_mb` (Number) — Cache budget assigned by the selected plan, in MiB; shared across routes. Read-only.
- `cache_max_object_kb` (Number) — Maximum object size assigned by the selected plan, in KiB. Read-only.
- `cache_size_limit_mb` (Number) — Current plan limit in MiB.
- `cache_effective_size_mb` (Number) — Cache budget assigned by the selected plan. Read-only.

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the gateway.
- `status` - Current status. One of: `creating`, `active`, `error`, `deleting`. PATCH operations apply in place and the row stays `active` throughout — there is no `updating` state in v1.
- `vip_address` - Private virtual IP address within the VNet — the pinned one, or the address picked by the platform.
- `vpc_name` - Name of the VPC the gateway is attached to.
- `vnet_name` - Name of the VNet the gateway is attached to.
- `vnet_cidr` - Address range (CIDR) of that VNet.
- `public_ip_address` - Public IPv4 address currently bound to the gateway, mirrored from the attached `ccp_public_ip`. Null while no IP is attached.
- `public_ip_status` - Lifecycle of the public IP attachment. One of: `allocated`, `attaching`, `attached`, `detaching`, `error`. Null when no IP is attached. The provider polls this field after an attach / detach so `terraform apply` exits only once the asynchronous IPaaS pipeline has converged.
- `error_message` - Last error message reported by the provisioner. Empty unless `status = error`.
- `created_at` - RFC 3339 creation timestamp.

## Import

Application Gateways can be imported using their UUID:

```
terraform import ccp_application_gateway.web <appgw_id>
```

## HTTP cache safety

Requires a platform version implementing cetic-cloud-platform#1733. Only public GET 200 responses populate the cache; HEAD may read a cached GET. Authorization, session cookies, Set-Cookie, private, no-store and no-cache bypass it. Unsupported Vary fields (including Accept-Language) bypass caching. Host, path, query string, route and gateway are isolated. Each replica owns its own cache; failover may reach a cold cache. Use the API, console or `cetic appgw cache purge` for invalidation; there is no Terraform purge resource.

## Migration to plan-managed cache (provider 8.0+)

The selected `plan` now determines both cache dimensions. Keep `cache_enabled`
and the route cache policies; remove the **assignments** `cache_size_mb = ...`
and `cache_max_object_kb = ...` from the resource configuration. References
such as `ccp_application_gateway.web.cache_size_mb` remain valid outputs.

Upgrade your provider constraint to `>= 8.0.0` once that version is released,
run `terraform init -upgrade`, then review `terraform plan`. Existing state
keys and types are retained: no resource replacement, state removal or import
is required for this change. A plan change updates dimensions in place.
The provider reads the profile from the API and never calculates its own sizes.

Coordinate this upgrade before deploying the plan-managed API. Provider 7.3
sends its own default dimensions even if they are omitted from HCL; when those
values differ from the new server profile, Terraform can report an inconsistent
result or persistent drift. Do not work around that with `ignore_changes` or
by repeatedly applying the old configuration. The new provider also requires
the plan-managed API for automatic resizing; an older API still returns its
legacy requested budget.
