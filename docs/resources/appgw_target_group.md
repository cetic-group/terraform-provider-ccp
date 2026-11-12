---
page_title: "ccp_appgw_target_group Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a backend pool (target group) on a CETIC Cloud Application Gateway.
---

# ccp_appgw_target_group (Resource)

Manages a target group on a [`ccp_application_gateway`](application_gateway.md). A target group is either:

- a **pool of servers** (`kind = "upstream"`, the default): load-balancing algorithm, L7 health check and optional session persistence, with members managed via [`ccp_appgw_target_group_member`](appgw_target_group_member.md); or
- a **bucket target** (`kind = "bucket"`): the gateway serves a public bucket, or one folder of it, under your own domain. Read-only (`GET` and `HEAD`), no listing of the content. No server, key, IP address or TLS option to provide.

Routes reference target groups via `target_group_id`.

## Example Usage

```hcl
resource "ccp_appgw_target_group" "api_pool" {
  appgw_id  = ccp_application_gateway.web.id
  name      = "api-pool"
  algorithm = "leastconn"

  hc_protocol            = "http"
  hc_method              = "GET"
  hc_path                = "/healthz"
  hc_expect_status       = 200
  hc_interval_sec        = 5
  hc_timeout_sec         = 3
  hc_healthy_threshold   = 2
  hc_unhealthy_threshold = 3

  sticky_enabled     = true
  sticky_cookie_name = "API_SESSION"
}
```

## Example Usage — Bucket target

```hcl
resource "ccp_object_bucket" "site" {
  name      = "acme-public-site"
  region    = "RNN"
  is_public = true
}

resource "ccp_appgw_target_group" "site" {
  appgw_id      = ccp_application_gateway.web.id
  name          = "site-assets"
  kind          = "bucket"
  bucket_id     = ccp_object_bucket.site.id
  bucket_prefix = "site/public" # optional: omit to serve the whole bucket
}

resource "ccp_appgw_listener" "assets" {
  appgw_id       = ccp_application_gateway.web.id
  hostname       = "assets.example.com"
  acme_challenge = "http01"
}

resource "ccp_appgw_route" "assets" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.assets.id
  target_group_id = ccp_appgw_target_group.site.id

  # Caching is set on the route, not on the target group.
  cache_mode          = "force"
  cache_default_ttl_s = 3600
  cache_max_ttl_s     = 3600
}
```

Then create **one DNS record** at your DNS host pointing `assets.example.com` to the gateway's public IP. The bucket must be public, active, and in the same region as the gateway; a private bucket is refused. A complete configuration is in [`examples/appgw-bucket-target`](https://github.com/cetic-group/terraform-provider-ccp/tree/main/examples/appgw-bucket-target).

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `name` - (Required) Target group name, unique per gateway (1-100 chars).

### Optional

- `kind` - (Optional, Forces new resource, default `upstream`) Target group type: `upstream` (a pool of servers) or `bucket` (a public bucket served by the gateway). Changing it replaces the target group.
- `bucket_id` - (Optional) UUID of the bucket to serve. **Required** with `kind = "bucket"`, rejected otherwise. Can be changed in place to serve another bucket (same conditions as at creation: public, active, same region as the gateway).
- `bucket_prefix` - (Optional) Folder of the bucket to serve, for example `site/public`. Default: the whole bucket. Letters, digits, `.`, `_`, `-` and `/` only, 512 characters at most, no `.` or `..` segment, and written without leading or trailing `/` (the plan tells you the expected form). Only with `kind = "bucket"`. Can be changed in place.

The following arguments describe a pool of servers and are **rejected at plan time with `kind = "bucket"`**: `algorithm`, `hc_*`, `sticky_enabled`, `sticky_cookie_name`. A bucket target has no members either: do not attach any `ccp_appgw_target_group_member` to it.

- `algorithm` - (Optional, default `roundrobin`) Load-balancing algorithm. One of: `roundrobin`, `leastconn`, `source` (client IP hash), `random`.
- `hc_protocol` - (Optional, default `http`) Health-check protocol. One of: `http`, `https`, `tcp`.
- `hc_method` - (Optional, default `GET`) HTTP method used for health checks. Ignored when `hc_protocol = tcp`.
- `hc_path` - (Optional, default `/`) Health-check URL path. Ignored when `hc_protocol = tcp`.
- `hc_expect_status` - (Optional, default `200`) Expected HTTP status from health-check responses.
- `hc_interval_sec` - (Optional, default `5`) Health-check interval in seconds.
- `hc_timeout_sec` - (Optional, default `3`) Per-check timeout in seconds.
- `hc_healthy_threshold` - (Optional, default `2`) Consecutive successful checks before a backend is marked healthy.
- `hc_unhealthy_threshold` - (Optional, default `3`) Consecutive failed checks before a backend is marked unhealthy.
- `sticky_enabled` - (Optional, default `false`) Enable cookie-based session stickiness.
- `sticky_cookie_name` - (Optional) Cookie name used when `sticky_enabled = true`. Defaults to `CCPAPPGWSESSID`.

## Attributes Reference

- `id` - UUID of the target group.
- `origin_status` - State of the bucket origin of a `bucket` target group: `ok` when the bucket is served, otherwise a problem described by `origin_error`. `null` for `upstream` target groups.
- `origin_error` - Reason why the bucket cannot be served (for example the bucket became private). `null` when there is no problem.

## Upgrading

A target group created with an earlier version of the provider is an `upstream` target group: the next `terraform plan` shows no change and nothing is replaced.

## Import

```
terraform import ccp_appgw_target_group.api_pool <appgw_id>/<target_group_id>
```
