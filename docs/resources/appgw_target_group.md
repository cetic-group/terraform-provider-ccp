---
page_title: "ccp_appgw_target_group Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a backend pool (target group) on a CETIC Cloud Application Gateway.
---

# ccp_appgw_target_group (Resource)

Manages a target group on a [`ccp_application_gateway`](application_gateway.md) — a pool of backends with load-balancing algorithm and L7 health-check configuration. Routes reference target groups via `target_group_id`. Members are managed via [`ccp_appgw_target_group_member`](appgw_target_group_member.md).

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

## Argument Reference

### Required

- `appgw_id` - (Required, Forces new resource) UUID of the parent `ccp_application_gateway`.
- `name` - (Required) Target group name, unique per gateway (1-100 chars).

### Optional

- `algorithm` - (Optional, default `roundrobin`) Load-balancing algorithm. One of: `roundrobin`, `leastconn`, `source` (client IP hash).
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

## Import

```
terraform import ccp_appgw_target_group.api_pool <appgw_id>/<target_group_id>
```
