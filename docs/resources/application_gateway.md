---
page_title: "ccp_application_gateway Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a CETIC Cloud Application Gateway (L7 HTTP/HTTPS reverse proxy with TLS, rate limiting and WAF).
---

# ccp_application_gateway (Resource)

Manages a CETIC Cloud **Application Gateway** (`ccp-appgw`) — a fully managed L7 HTTP/HTTPS reverse proxy that terminates TLS, multiplexes hostnames via SNI, applies per-route policies (rate limit, IP allow/deny, CORS, basic auth, WAF) and load-balances to a pool of backends.

By default each gateway uses two instances with a floating virtual IP and automatic failover. Set `replica_count = 1` for a single instance without redundancy. Listeners, routes and target groups are declared as separate resources to keep per-route HCL edits idempotent:

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
- `replica_count` (Number, Optional) — `1` without redundancy or `2` for high availability (default). Changing it forces replacement.
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

## Migration to plan-managed cache (provider 7.3.2+)

The selected `plan` now determines both cache dimensions. Keep `cache_enabled`
and the route cache policies; remove the **assignments** `cache_size_mb = ...`
and `cache_max_object_kb = ...` from the resource configuration. References
such as `ccp_application_gateway.web.cache_size_mb` remain valid outputs.

Upgrade the provider to `7.3.2` or later (for example `~> 7.3, >= 7.3.2`),
run `terraform init -upgrade`, then review `terraform plan`. Existing state
keys and types are retained: no resource replacement, state removal or import
is required for this change. A plan change updates dimensions in place.
The provider reads the profile from the API and never calculates its own sizes.

The plan-managed API is already deployed. Provider 7.3.1 and earlier send their
own default dimensions even if they are omitted from HCL; when those values
differ from the server profile, Terraform can report an inconsistent result or
persistent drift. Do not work around that with `ignore_changes` or
by repeatedly applying the old configuration. The new provider also requires
the plan-managed API for automatic resizing; an older API still returns its
legacy requested budget.

## Number of instances

`replica_count` is optional and defaults to `2` (high availability). Set it to
`1` for an environment that accepts interruption when its only instance is
unavailable or under maintenance. The capacity plan remains independent of
this choice; capacity and cache budgets are per instance.

The value is immutable after creation. Changing it forces replacement of the
resource, including when reverting from `1` to the default. Existing resources
continue to use `2`. Set the matching value when importing a single instance.
The platform catalog defines independent prices for one and two instances.
Each amount already covers the selected mode; the provider does not multiply
it by the instance count.

Requires a platform implementing cetic-cloud-platform#1748. Upgrade the
platform before requesting the new mode. The provider can be installed first
while keeping the historical value `2`. An older platform may ignore the
field: if the returned mode differs, creation fails explicitly and preserves
the created ID in Terraform state so the resource is not orphaned.

The HTTP cache is an optional paid feature. `cache_enabled` remains mutable
and defaults to `false`. Its surcharge is published under `appgw_cache` for
the selected plan and instance count; enabling it opts into the published
surcharge. Use the platform estimator for the combined price. An unavailable
cache tariff must be configured on the platform before enabling the option.

## HTTP access policies (provider 7.3.2+)

Requires provider `>= 7.3.2` and a platform advertising
`appgw_security_policy_versions = [1]`.
The provider checks this capability before submitting a policy, including `{}`.
An older server cannot silently ignore the configuration. Nonempty documents
also require `appgw_security_policies_enabled = true`. Explicit `{}` resets
remain available when new policy activation is disabled.

- `l7_policy` - (Optional, Computed) HTTP access policies. Omit to preserve the
  remote policy, including after import. Set `{}` explicitly to clear the local
  policy. Removing the attribute relinquishes management without disabling
  remote protection. Changing this object updates the resource in place.
- `enabled` - (Optional, default `false`) Enable the policies in this object.
- `policies` - (Optional) Map of at most eight policies, keyed by stable names.
  Names begin with a lowercase letter and contain at most 32 lowercase letters,
  digits, underscores or hyphens.
- `scope` - (Optional) Independent protected request scope; omission covers all
  requests reaching this owner. A route policy also covers the route's path
  independently of its method/header routing selectors.
- `path` - (Optional, inside a scope or rule match) Path condition evaluated on
  the original request before rewrites. Ambiguous encodings are rejected.
- `op` - (Required for a path or header condition) Paths: `exact` or
  `segment_prefix` (`/admin` matches `/admin/users`, not `/administrator`).
  Headers: `present`, `absent`, `non_empty`, `eq`, `in`.
- `value` - (Required for a path condition) Path beginning with `/`.
- `allow_rules` - (Optional) Map of rules, combined with OR. If nonempty, at
  least one must match within the policy scope. Omission means no allow filter.
- `deny_rules` - (Optional) Map of rules, combined with OR; a matching deny wins.
- `match` - (Required in every rule) All supplied criteria combine with AND.
- `source_cidrs` - (Optional) Nonempty list of IPv4/IPv6 addresses or networks,
  at most 256 entries. Uses the gateway's `client_ip_policy` interpretation.
- `methods` - (Optional) Nonempty list of at most 16 uppercase HTTP methods.
- `headers` - (Optional) List of one to eight header conditions, combined with AND.
- `match_all` - (Optional) Explicit unconditional rule: only `true`, without any
  other criterion. It still applies only within the policy's independent scope.
- `required_headers` - (Optional) Map of at most eight mandatory header conditions,
  combined with AND independently of allow rules. A matching IP allow cannot
  bypass a required header. Keys must be unique across this policy's allow,
  deny and required-header maps.
- `name` - (Required in a header condition) Case-insensitive HTTP header name,
  at most 64 characters.
- `values` - (Optional in a header condition) Case-sensitive exact ASCII scalars,
  at most 16 values of 256 bytes. `eq` requires one; `in` requires one or more;
  presence operations accept none. Empty values, commas and surrounding
  whitespace are rejected. Examined duplicate headers are rejected too.
- `security_revision` - (Computed) Gateway-wide revision observed for this
  resource. The provider obtains the current revision for each policy write;
  users do not set it. Concurrent changes return a conflict: refresh and review
  the plan before applying again. Conflicts are not retried automatically.

The aggregate limit is 64 allow and deny rules across the gateway,
listeners and routes. Mandatory header requirements have a separate limit of
eight per policy. Applicable policies intersect. A local allow cannot
bypass an inherited deny, legacy network restrictions or authentication.
Header checks are access filters, not a substitute for authentication; avoid
storing credentials in conditions. Terraform state contains policy values.

A hostname protected by any HTTP policy cannot contain regex routes. Header
conditions also require `cache_mode = "off"` on every overlapping route,
including when gateway caching is currently disabled. Disjoint scopes can
keep caching. Update each conflicting route and reapply the desired parent
policy; for a route's own policy, the cache change and policy can share a PATCH.
The API validates the whole gateway, including sibling resources.

Terraform waits for the accepted generation to be applied to every expected
instance, with old connections revoked, and verifies that managed policy fields
still match. A newer generation from an unrelated resource is accepted only
when these conditions remain true. An `active` gateway or accepted PATCH alone
is insufficient. Timeouts and conflicts keep the resource ID in state; refresh
and inspect the configuration before retrying. Deleting a child on a gateway
with tracked policies also waits for the committed deletion generation and
absence of that child.

### TCP filtering and client addresses

- `l4_policy` - (Optional, Computed) Filter TCP peers on ports 80 and 443, before
  HTTP and TLS. Omit to preserve, `{}` to clear, remove to relinquish management.
- `tcp_80` - (Optional in `l4_policy`) Restrictions for TCP port 80.
- `tcp_443` - (Optional in `l4_policy`) Restrictions for TCP port 443.
- `allow_cidrs` - (Optional in either TCP object) At most 256 IPv4/IPv6 addresses
  or prefixes. An empty list applies no allow restriction.
- `deny_cidrs` - (Optional in either TCP object) At most 256 denied addresses or
  prefixes. Deny takes precedence over allow.
- `client_ip_policy` - (Optional, Computed) Client address interpretation for
  new HTTP policies. Omit to preserve, `{}` to restore peer interpretation.
- `mode` - (Optional, default `peer`) `peer` uses the TCP peer. `trusted_xff`
  accepts a forwarded client address only through explicit trusted networks.
- `trusted_proxy_cidrs` - (Optional) At most 256 trusted IPv4/IPv6 networks.
  Required and nonempty for `trusted_xff`, empty for `peer`. Trust only proxy
  networks you control. Malformed or ambiguous trusted forwarded chains fail
  closed; an untrusted peer's forwarding headers do not establish identity.
- `access_policy_state` - (Computed) Public application state: `pending`,
  `partial`, `applied` or `unknown`. This is distinct from gateway `status`.

`l4_policy.enabled` defaults to false. TCP filtering and historical
`global_allow_cidrs`, `global_deny_cidrs` and route `allow_cidrs` / `deny_cidrs`
continue to use the peer address. `trust_proxy_headers` controls legacy header
forwarding and does not configure the trusted proxy policy.

Restricting TCP port 80 is incompatible with a listener using HTTP-01,
including its default challenge. Use DNS-01 or a custom certificate before
restricting that port. The API rejects incompatible combinations rather than
silently bypassing the filter for certificate renewal.

```hcl
l4_policy = {
  enabled = true
  tcp_443 = { allow_cidrs = ["192.0.2.0/24", "2001:db8::/32"] }
}
client_ip_policy = { mode = "peer" }
l7_policy = {
  enabled = true
  policies = {
    administration = {
      scope = { path = { op = "segment_prefix", value = "/admin" } }
      allow_rules = {
        office_read = { match = { source_cidrs = ["192.0.2.0/24"], methods = ["GET", "HEAD"] } }
      }
      required_headers = {
        request_id = { name = "X-Request-ID", op = "non_empty" }
      }
    }
  }
}
```
