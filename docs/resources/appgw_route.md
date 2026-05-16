---
page_title: "ccp_appgw_route Resource - cetic-cloud-platform"
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
