---
page_title: "ccp_load_balancer Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a load balancer on CETIC Cloud Platform.
---

# ccp_load_balancer (Resource)

Manages a CETIC Cloud Load Balancer. Listeners (TCP/HTTP/HTTPS) with weighted backends (container or VM instances) are declared in the resource and sent at creation time. HTTPS listeners can obtain a Let's Encrypt certificate automatically via ACME (`http01`/`dns01`). Supports public IP attachment via `public_ip_id`.

~> **Note:** Load balancer provisioning is asynchronous. The provider polls until the load balancer reaches `active` status (up to 5 minutes).

~> **Listeners are immutable.** Any change to a listener attribute other than its backends (e.g. protocol, port, algorithm, domain, ACME settings) forces replacement of the entire load balancer. Backends can be added, removed or updated in place.

## Example Usage

```hcl
resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

resource "ccp_vnet" "front" {
  vpc_id = ccp_vpc.prod.id
  name   = "front"
  cidr   = "10.0.1.0/24"
  snat   = true
}

resource "ccp_public_ip" "lb" {
  region = "RNN"
}

resource "ccp_container_instance" "web" {
  name     = "web-1"
  region   = "RNN"
  plan     = "small"
  template = "ubuntu-24.04"
  vnet_id  = ccp_vnet.front.id
}

resource "ccp_load_balancer" "web" {
  name    = "web-lb"
  region  = "RNN"
  vnet_id = ccp_vnet.front.id
  plan    = "small"

  public_ip_id = ccp_public_ip.lb.id

  # HTTPS listener with automatic Let's Encrypt certificate (HTTP-01 challenge)
  listener {
    protocol    = "https"
    listen_port = 443
    algorithm   = "roundrobin"

    domain         = "www.example.com"
    acme_challenge = "http01"

    backend {
      container_id = ccp_container_instance.web.id
      port         = 8080
    }
  }

  # HTTP listener (e.g. for redirect or plain HTTP traffic)
  listener {
    protocol    = "http"
    listen_port = 80

    backend {
      container_id = ccp_container_instance.web.id
      port         = 8080
    }
  }
}

output "lb_vip" {
  value = ccp_load_balancer.web.vip_address
}

output "lb_public_ip" {
  value = ccp_load_balancer.web.public_ip_address
}
```

### DNS-01 challenge (customer-owned domain via DNS provider)

```hcl
data "ccp_acme_dns_providers" "all" {}

# Use data.ccp_acme_dns_providers.all.providers to discover valid
# acme_dns_provider keys and the credential fields each expects.

variable "cloudflare_token" {
  type      = string
  sensitive = true
}

resource "ccp_load_balancer" "api" {
  name    = "api-lb"
  region  = "RNN"
  vnet_id = ccp_vnet.front.id

  listener {
    protocol    = "https"
    listen_port = 443

    domain             = "api.example.com"
    acme_challenge     = "dns01"
    acme_dns_provider  = "cloudflare"
    acme_dns_credentials = {
      api_token = var.cloudflare_token
    }

    backend {
      container_id = ccp_container_instance.api.id
      port         = 8080
    }
  }
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name (1-100 chars).
- `region` - (Required, Forces new resource) Region where the load balancer is created. One of: `RNN`, `PAR`, `ABJ`.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the load balancer's virtual IP (VIP) will be hosted.

### Optional

- `plan` - (Optional, Forces new resource) Capacity plan. One of: `small` (default), `medium`, `large`. Changing the plan forces replacement.
- `public_ip_id` - (Optional) UUID of a `ccp_public_ip` to attach as the public entrypoint. Remove to detach.
- `tags` - (Optional) Free-form tags (max 60 tags, max 50 chars each).
- `listener` - (Optional) One or more `listener` blocks. See [Listener Reference](#listener-reference) below.

## Listener Reference

Each `listener` block supports:

### Required

- `protocol` - (Required, Immutable) Protocol. One of: `tcp`, `http`, `https`.
- `listen_port` - (Required, Immutable) Port the load balancer listens on (1-65535).

### Optional

- `algorithm` - (Optional, Immutable) Load-balancing algorithm. One of: `roundrobin` (default), `leastconn`, `source`.
- `health_check_enabled` - (Optional, Immutable) Enable backend health checks. Defaults to `true`.
- `health_check_path` - (Optional, Immutable) HTTP path used for health checks (for `http`/`https` listeners).
- `domain` - (Optional, Immutable) Fully-qualified domain name served by an `https` listener. Required when `acme_challenge` is set. Must be lowercase.
- `acme_challenge` - (Optional, Immutable) ACME (Let's Encrypt) challenge type: `http01` or `dns01`. Requires `protocol = "https"` and `domain`. `dns01` additionally requires `acme_dns_provider` and `acme_dns_credentials`.
- `acme_dns_provider` - (Optional, Immutable) DNS provider key for `dns01` (e.g. `cloudflare`, `route53`). See the [`ccp_acme_dns_providers`](../data-sources/acme_dns_providers.md) data source for the supported catalog.
- `acme_dns_credentials` - (Optional, Sensitive, Immutable) DNS provider credentials for `dns01` (write-only — never returned by the API). Keys depend on the provider (see `ccp_acme_dns_providers`).
- `backend` - (Optional) One or more `backend` blocks. Backends can be added, removed or updated in place without replacing the load balancer. See [Backend Reference](#backend-reference) below.

### Computed

- `id` - The UUID of the listener.
- `acme_status` - ACME certificate status: `pending` | `issuing` | `issued` | `renewing` | `error`.
- `acme_last_error` - Last certificate issuance error, if any. Cleared when issuance succeeds.

### Important semantics

- **Listeners are sent in the initial create request.** The API does not support adding or modifying listeners after creation. Any change to an immutable listener field forces replacement of the whole load balancer.
- **Backends are reconciled in place.** Adding, removing, or changing `weight` on a backend does not replace the load balancer.
- **ACME requires** `protocol = "https"` and `domain`. For `dns01`, `acme_dns_provider` and `acme_dns_credentials` are also required.
- `acme_status` and `acme_last_error` are read-only and reflect certificate issuance progress. A freshly created listener starts in `pending` and transitions to `issued` once the certificate is obtained.

## Backend Reference

Each `backend` block inside a `listener` supports:

### Required (exactly one)

- `container_id` - (Optional) UUID of a container instance to route traffic to.
- `vm_instance_id` - (Optional) UUID of a VM instance to route traffic to.

### Required

- `port` - (Required) Backend port (1-65535).

### Optional

- `weight` - (Optional) Backend weight for weighted load balancing (0-256). Defaults to `1`. Weight changes are reconciled in place.

### Computed

- `id` - The UUID of the backend.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the load balancer.
- `status` - Current status: `provisioning`, `active`, `updating`, or `error`.
- `vip_address` - Private virtual IP address within the VNet.
- `public_ip_address` - Public IP address if one is attached, otherwise empty.
- `created_at` - RFC 3339 creation timestamp.

## Import

Load balancers can be imported using their UUID:

```
terraform import ccp_load_balancer.web <lb_id>
```

~> **Note:** After import, `acme_dns_credentials` cannot be recovered (write-only). If an imported listener uses `dns01`, declaring `acme_dns_credentials` in configuration will not cause a replacement — the provider carries over the prior credentials value from state across reads.
