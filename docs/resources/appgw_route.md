---
page_title: "ccp_appgw_route Resource - ccp"
subcategory: "Networking"
description: |-
  Manages an L7 route (condition + policies) on an Application Gateway listener.
---

# ccp_appgw_route (Resource)

Manages a single L7 route on a [`ccp_appgw_listener`](appgw_listener.md). A route is a `(path + headers + methods)` condition plus L7 policies — rate limit, IP allow/deny, CORS, basic auth, WAF preset, request and response header injection. The route forwards matched traffic to a [`ccp_appgw_target_group`](appgw_target_group.md).

Routes are evaluated in ascending `priority` order — the first match wins.

~> **`basic_auth_user.password` is Sensitive.** Plaintext values are persisted in the Terraform state — keep your state backend secure. The platform never returns plaintext back: the server hashes the values into a Secret Manager entry (`basic_auth_secret_ref`).

## Example Usage — Basic prefix route

```hcl
resource "ccp_appgw_route" "api_v1" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.api.id
  priority        = 10
  path_match      = "/v1/"
  path_match_type = "prefix"
  target_group_id = ccp_appgw_target_group.api_pool.id
}
```

## Example Usage — Prefix-stripped route

When the backend expects requests without the routing prefix (e.g. a sub-app
mounted at `/web-app` that internally serves `/`), set `strip_prefix = true`
to have the gateway remove `path_match` from the path before forwarding.

```hcl
resource "ccp_appgw_route" "web_app" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.public.id
  priority        = 10
  path_match      = "/web-app"
  path_match_type = "prefix"
  strip_prefix    = true   # /web-app/foo → /foo on the backend
  target_group_id = ccp_appgw_target_group.web_pool.id
}
```

## Example Usage — Route with full policy stack

```hcl
resource "ccp_appgw_route" "api_admin" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.api.id
  priority        = 5
  path_match      = "/admin/"
  path_match_type = "prefix"
  method_match    = ["GET", "POST", "PUT", "DELETE"]
  target_group_id = ccp_appgw_target_group.api_pool.id

  rate_limit_per_sec = 20
  allow_cidrs        = ["10.0.0.0/8", "192.168.0.0/16"]
  deny_cidrs         = []

  request_headers = {
    "X-Real-IP" = "%[src]"
  }
  response_headers = {
    "X-Frame-Options" = "DENY"
  }

  cors_enabled     = true
  cors_origins     = ["https://app.example.com"]
  cors_methods     = ["GET", "POST"]
  cors_credentials = true

  basic_auth_user {
    user     = "admin"
    password = var.admin_password
  }
  basic_auth_user {
    user     = "ops"
    password = var.ops_password
  }

  waf_preset = "strict"

  header_match {
    name  = "X-Tenant"
    value = "acme-corp"
    op    = "eq"
  }
}
```

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `listener_id` - (Required, Forces new resource) UUID of the `ccp_appgw_listener` (hostname) this route applies to.
- `target_group_id` - (Required) UUID of the `ccp_appgw_target_group` that receives matched traffic.

### Optional

- `cache_mode` (String, Optional) — `off` (default), `respect` or `force`. Force adds freshness only when the origin provides none; private directives remain authoritative.
- `cache_default_ttl_s` (Number, Optional) — Force-mode fallback lifetime, 1–3600 seconds (default `60`). Must not exceed `cache_max_ttl_s`.
- `cache_max_ttl_s` (Number, Optional) — Maximum lifetime, 1–3600 seconds (default `3600`).
- `cache_bypass_cookies` (Set of String, Optional) — Session cookies to exclude. An empty list bypasses any Cookie header. With an explicit list, other cookies must not personalize public responses.
- `cache_debug_headers` (Boolean, Optional) — Exposes `X-Cache: HIT` or `MISS` (default `false`).

- `priority` - (Optional, default `100`) Evaluation priority (lower = earlier). Two routes with the same priority on the same listener have undefined ordering — keep them unique.
- `path_match` - (Optional) Path expression to match (e.g. `/api/`, `/v1/users`, `^/[a-z]+/[0-9]+`). Omit to match all paths.
- `path_match_type` - (Optional, default `prefix`) How `path_match` is interpreted. One of: `prefix`, `exact`, `regex`.
- `method_match` - (Optional) List of HTTP methods to match (e.g. `["GET", "POST"]`). Empty matches all methods.
- `rate_limit_per_sec` - (Optional) Per-IP rate limit in req/sec. Null inherits the gateway-wide limit.
- `allow_cidrs` - (Optional) List of CIDRs allowed to hit this route. Empty list = allow all (subject to the gateway-wide `global_allow_cidrs`).
- `deny_cidrs` - (Optional) List of CIDRs denied access to this route. Evaluated before allow.
- `request_headers` - (Optional) Map of headers to set on the request before it reaches the backend.
- `response_headers` - (Optional) Map of headers to set on the response before it leaves the gateway.
- `cors_enabled` - (Optional, default `false`) Enable CORS for this route.
- `cors_origins` - (Optional) List of origins allowed when `cors_enabled = true` (e.g. `["https://app.example.com"]` or `["*"]`).
- `cors_methods` - (Optional) List of methods allowed when `cors_enabled = true`.
- `cors_credentials` - (Optional, default `false`) When `true`, sends `Access-Control-Allow-Credentials: true`.
- `waf_preset` - (Optional, default `off`) WAF preset enforced on this route. One of: `off`, `permissive`, `strict`.
- `strip_prefix` - (Optional, default `false`) When `true` and `path_match` is non-empty (prefix or exact mode), the gateway strips the `path_match` prefix before forwarding to the backend. E.g. with `path_match = "/web-app"` and `strip_prefix = true`, an incoming request to `/web-app/foo` reaches the backend as `/foo`. Ignored when `path_match` is empty or when `path_match_type = "regex"`.
- `header_match` - (Optional, nested block) Match a request header. Each block adds an AND condition.
- `basic_auth_user` - (Optional, nested block) User credential pair for HTTP Basic authentication. Declaring at least one block enables basic auth for the route. **Omitting** the block entirely on `terraform apply` preserves the existing basic auth configuration; passing an **empty list** (no blocks where some were declared previously) explicitly clears it.

## Nested Block — `header_match`

- `name` - (Required) Header name (case-insensitive, 1-100 chars).
- `value` - (Required) Expected value (interpretation depends on `op`).
- `op` - (Optional, default `eq`) Comparison operator. One of: `eq`, `prefix`, `regex`.

## Nested Block — `basic_auth_user`

- `user` - (Required) Username (1-64 chars). Maps to the `user` field on the API.
- `password` - (Required, **Sensitive**) Plaintext password (1-128 chars). The platform bcrypts each value and stores the list as an encrypted Secret Manager entry referenced by `basic_auth_secret_ref`. **The server never echoes plaintext back** — `terraform plan` will appear to want to "re-set" the passwords on every run only if the state file is dropped; otherwise the local state is the source of truth.

## Attributes Reference

- `id` - UUID of the route.
- `basic_auth_secret_ref` - (Sensitive) Server-generated reference to the Secret Manager entry storing the hashed basic-auth users.

## Import

```
terraform import ccp_appgw_route.api_v1 <appgw_id>/<route_id>
```

~> After import, `basic_auth_user` is empty in state — the platform never returns plaintext passwords. Run `terraform apply` with the expected `basic_auth_user` blocks to reconcile.

## HTTP cache safety

Requires a platform version implementing cetic-cloud-platform#1733. Only public GET 200 responses populate the cache; HEAD may read a cached GET. Authorization, session cookies, Set-Cookie, private, no-store and no-cache bypass it. Unsupported Vary fields (including Accept-Language) bypass caching. Host, path, query string, route and gateway are isolated. Each replica owns its own cache; failover may reach a cold cache. Use the API, console or `cetic appgw cache purge` for invalidation; there is no Terraform purge resource.

## HTTP access policies (provider 8.1+)

Requires provider `>= 8.1.0` once released and a platform advertising
`appgw_security_policy_versions = [1]`. This change does not publish a release.
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

The aggregate limit is 64 rules and header requirements across the gateway,
listeners and routes. Applicable policies intersect. A local allow cannot
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
