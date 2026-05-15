---
page_title: "ccp_application_gateway Resource - cetic-cloud-platform"
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
  plan         = "medium"
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

## Argument Reference

### Required

- `name` - (Required) Human-readable name of the gateway (1-100 chars).
- `region` - (Required, Forces new resource) Region code (`RNN`, `PAR`, `ABJ`).
- `plan` - (Required) Capacity plan. One of: `small`, `medium`, `large`.
- `vpc_id` - (Required, Forces new resource) UUID of the VPC the gateway is provisioned in.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet the gateway VIP is hosted on. Backends declared via target group members must be reachable from this VNet.

### Optional

- `public_ip_id` - (Optional) UUID of a `ccp_public_ip` to attach as the public entrypoint. Set to attach, remove to detach.
- `force_https` - (Optional, default `true`) Redirect plain HTTP (`:80`) traffic to HTTPS (`:443`).
- `hsts_enabled` - (Optional, default `false`) Send the `Strict-Transport-Security` header on every HTTPS response.
- `hsts_max_age` - (Optional, default `31536000`) `max-age` directive of the HSTS header in seconds.
- `global_rate_limit_per_sec` - (Optional) Gateway-wide rate limit (req/sec/IP). Null disables the global limit — routes can still set their own.
- `global_allow_cidrs` - (Optional) List of CIDRs allowed to hit any route. Empty = allow all.
- `global_deny_cidrs` - (Optional) List of CIDRs denied access — evaluated before allow.
- `trust_proxy_headers` - (Optional, default `false`) Accept incoming `X-Forwarded-For` / `X-Real-IP` headers from clients.
- `tags` - (Optional) Free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the gateway.
- `status` - Current status: `creating`, `active`, `updating`, `error`, `deleting`.
- `vip_address` - Private virtual IP address within the VNet.
- `error_message` - Last error message reported by the provisioner. Empty unless `status = error`.
- `created_at` - RFC 3339 creation timestamp.

## Import

Application Gateways can be imported using their UUID:

```
terraform import ccp_application_gateway.web <appgw_id>
```
