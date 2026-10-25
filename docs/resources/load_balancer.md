---
page_title: "ccp_load_balancer Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a load balancer on CETIC Cloud Platform.
---

# ccp_load_balancer (Resource)

Manages a CETIC Cloud Load Balancer. Listeners (TCP/HTTP/HTTPS) with weighted backends (container or VM instances) are declared in the resource and sent at creation time. HTTPS listeners can obtain a Let's Encrypt certificate automatically via ACME (`http01`/`dns01`). Supports public IP attachment via `public_ip_id`.

~> **Note:** Load balancer provisioning is asynchronous. The provider polls until the load balancer reaches `active` status (up to 5 minutes).

~> **Listeners are immutable.** Any change to a listener attribute other than its backends and its own certificate (e.g. protocol, port, algorithm, domain, ACME settings) forces replacement of the entire load balancer. Backends can be added, removed or updated in place, and a certificate you provide (`acme_challenge = "custom"`) is rotated in place.

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
  name          = "web-1"
  region        = "RNN"
  plan          = "small"
  template      = "ubuntu-24.04"
  vnet_id       = ccp_vnet.front.id
  root_password = var.root_password
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

The `ionos` provider expects `prefix` and `secret` credentials:

```hcl
variable "ionos_prefix" {
  type      = string
  sensitive = true
}

variable "ionos_secret" {
  type      = string
  sensitive = true
}

resource "ccp_load_balancer" "api_ionos" {
  name    = "api-lb-ionos"
  region  = "RNN"
  vnet_id = ccp_vnet.front.id

  listener {
    protocol    = "https"
    listen_port = 443

    domain             = "api.example.com"
    acme_challenge     = "dns01"
    acme_dns_provider  = "ionos"
    acme_dns_credentials = {
      prefix = var.ionos_prefix
      secret = var.ionos_secret
    }

    backend {
      container_id = ccp_container_instance.api.id
      port         = 8080
    }
  }
}
```

### Your own certificate, a custom error page and a scale set backend

```hcl
resource "ccp_container_scale_set" "api" {
  name              = "api"
  region            = "RNN"
  plan              = "small"
  template          = "ubuntu-24.04"
  vnet_id           = ccp_vnet.front.id
  min_instances     = 2
  desired_instances = 3
  max_instances     = 6
  root_password     = var.root_password
}

resource "ccp_load_balancer" "shop" {
  name    = "shop-lb"
  region  = "RNN"
  vnet_id = ccp_vnet.front.id

  # Served instead of the default page when no backend answers (502/503/504).
  custom_error_page_html = file("${path.module}/maintenance.html")

  listener {
    protocol    = "https"
    listen_port = 443
    domain      = "shop.example.com"

    # Your own certificate. Changing either file rotates it in place.
    acme_challenge      = "custom"
    tls_certificate_pem = file("${path.module}/fullchain.pem")
    tls_private_key_pem = file("${path.module}/privkey.pem")

    # Dynamic target: every running instance of the scale set, including
    # instances added later, receives traffic without a new apply.
    backend {
      container_scale_set_id = ccp_container_scale_set.api.id
      port                   = 8080
    }
  }
}

output "shop_cert_expiry" {
  value = ccp_load_balancer.shop.listener[0].tls_cert_not_after
}
```

### Pinned private address

Reserve the address, then hand it to the load balancer. Keep it outside the address
block of any Kubernetes cluster sharing the VNet.

```hcl
resource "ccp_vnet_ip_reservation" "api_vip" {
  vnet_id     = ccp_vnet.web.id
  name        = "api-vip"
  ip          = "10.0.1.200"
  description = "Private address of the load balancer"
}

resource "ccp_load_balancer" "api" {
  name        = "api-lb"
  region      = "RNN"
  vnet_id     = ccp_vnet.web.id
  vip_address = ccp_vnet_ip_reservation.api_vip.ip
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable name (1-100 chars).
- `region` - (Required, Forces new resource) Region where the load balancer is created. One of: `RNN`, `PAR`, `ABJ`.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the load balancer's virtual IP (VIP) will be hosted.

### Optional

- `plan` - (Optional, Forces new resource) Capacity plan key, e.g. `small` (default), `medium`, `large`. Resolved server-side against the live plan catalog (`compute_plans`, key `lb-<plan>`, kind `lb`) — no hardcoded list, so plans added to the catalogue are usable without a provider release. Changing the plan forces replacement.
- `replica_count` (Number, Optional) — `1` without redundancy or `2` for high availability (default). Changing it forces replacement.
- `public_ip_id` - (Optional) UUID of a `ccp_public_ip` to attach as the public entrypoint. Remove to detach.
- `vip_address` - (Optional, Forces new resource) Private IPv4 address of the load balancer in the VNet, in canonical dotted form (`10.0.1.200`). When omitted, the platform picks a free address and exports it. Set it to pin the address: reserve it first with `ccp_vnet_ip_reservation` (a reservation of your own organization is accepted). The API rejects an address outside the VNet or below the start of the network's address range, an address reserved for the network itself (network, gateway `.1`, broadcast), or one already in use — including the address block reserved for the nodes of a Kubernetes cluster on the same VNet, and an address reserved by another organization on a shared network.
- `tags` - (Optional) Free-form tags (max 60 tags, max 50 chars each).
- `custom_error_page_html` - (Optional) Your own HTML page, served instead of the default one when no backend can answer (HTTP 502, 503 and 504). At most 7 KiB (UTF-8 bytes) once leading and trailing whitespace is trimmed — inline the styles and host images elsewhere. Updated in place; **removing the attribute restores the default page**. Leading/trailing whitespace (e.g. the final newline of a heredoc or `file()`) does not cause a diff.
- `listener` - (Optional) One or more `listener` blocks. See [Listener Reference](#listener-reference) below.

## Listener Reference

Each `listener` block supports:

### Required

- `protocol` - (Required, Immutable) Protocol. One of: `tcp`, `http`, `https`.
- `listen_port` - (Required, Immutable) Port the load balancer listens on (1-65535).

### Optional

- `algorithm` - (Optional, Immutable) Load-balancing algorithm. One of: `roundrobin` (default), `leastconn`, `source`, `random`.
- `health_check_enabled` - (Optional, Immutable) Enable backend health checks. Defaults to `true`.
- `health_check_path` - (Optional, Immutable) HTTP path used for health checks (for `http`/`https` listeners).
- `domain` - (Optional, Immutable) Fully-qualified domain name served by an `https` listener. Required when `acme_challenge` is set. Must be lowercase.
- `acme_challenge` - (Optional, Immutable) How the certificate of an `https` listener is obtained: `http01` or `dns01` (Let's Encrypt; requires `domain`, and `dns01` additionally requires `acme_dns_provider` and `acme_dns_credentials`), or `custom` (your own certificate, see below).
- `tls_certificate_pem` - (Optional, Sensitive) With `acme_challenge = "custom"`: your certificate followed by its chain, PEM-encoded. Checked at submission: matching key, validity, and — when `domain` is set — that the certificate covers it (without `domain`, no name check is made). **Changing it rotates the certificate in place**, without replacing the listener or the load balancer. Never returned by the API.
- `tls_private_key_pem` - (Optional, Sensitive) With `acme_challenge = "custom"`: the private key, PEM-encoded and **not** protected by a passphrase. Write-only: the value you provide is kept in state, it is never read back. Changing it rotates the certificate in place.
- `acme_dns_provider` - (Optional, Immutable) DNS provider key for `dns01` (e.g. `cloudflare`, `route53`, `ionos`). See the [`ccp_acme_dns_providers`](../data-sources/acme_dns_providers.md) data source for the supported catalog.
- `acme_dns_credentials` - (Optional, Sensitive, Immutable) DNS provider credentials for `dns01` (write-only — never returned by the API). Keys depend on the provider (see `ccp_acme_dns_providers`).
- `backend` - (Optional) One or more `backend` blocks. Backends can be added, removed or updated in place without replacing the load balancer. See [Backend Reference](#backend-reference) below.

### Computed

- `id` - The UUID of the listener.
- `acme_status` - ACME certificate status: `pending` | `issuing` | `issued` | `renewing` | `error`.
- `acme_last_error` - Last certificate issuance error, if any. Cleared when issuance succeeds.
- `tls_cert_not_after` - Expiry date of your certificate (`acme_challenge = "custom"`). A certificate you provide is **not** renewed automatically: rotate it before this date.
- `tls_cert_names` - Names covered by your certificate.
- `tls_cert_issuer` - Issuer of your certificate.

### Important semantics

- **Listeners are sent in the initial create request.** The API does not support adding or modifying listeners after creation. Any change to an immutable listener field forces replacement of the whole load balancer.
- **Backends are reconciled in place.** Adding, removing, or changing `weight` on a backend does not replace the load balancer.
- **ACME requires** `protocol = "https"` and `domain`. For `dns01`, `acme_dns_provider` and `acme_dns_credentials` are also required.
- **`acme_challenge = "custom"`** requires `protocol = "https"`, `tls_certificate_pem` and `tls_private_key_pem` (checked at plan time). Switching between `custom` and Let's Encrypt changes `acme_challenge`, which is immutable and replaces the load balancer.
- `acme_status` and `acme_last_error` are read-only and reflect certificate issuance progress. A freshly created listener starts in `pending` and transitions to `issued` once the certificate is obtained.

## Backend Reference

Each `backend` block inside a `listener` supports:

### Required (exactly one)

- `container_id` - (Optional) UUID of a container instance to route traffic to.
- `vm_instance_id` - (Optional) UUID of a VM instance to route traffic to.
- `container_scale_set_id` - (Optional) UUID of a container scale set. **Dynamic target**: every running instance receives traffic, including instances added later — no new apply needed after a scale-up.
- `vm_scale_set_id` - (Optional) UUID of a VM scale set. Dynamic target, like `container_scale_set_id`.

Exactly one of the four must be set (checked at plan time).

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
- `vip_address` - Private virtual IP address within the VNet — the pinned one, or the address picked by the platform.
- `public_ip_address` - Public IP address if one is attached, otherwise empty.
- `vpc_id` - UUID of the VPC that contains `vnet_id`.
- `vpc_name` - Name of that VPC.
- `vnet_name` - Name of the VNet the load balancer is attached to.
- `vnet_cidr` - Address range (CIDR) of that VNet.
- `created_at` - RFC 3339 creation timestamp.

## Import

Load balancers can be imported using their UUID:

```
terraform import ccp_load_balancer.web <lb_id>
```

~> **Note:** After import, `acme_dns_credentials` cannot be recovered (write-only). If an imported listener uses `dns01`, declaring `acme_dns_credentials` in configuration will not cause a replacement — the provider carries over the prior credentials value from state across reads.

## Number of instances

`replica_count` is optional and defaults to `2` (high availability). Set it to
`1` for an environment that accepts interruption when its only instance is
unavailable or under maintenance. The capacity plan remains independent of
this choice; capacity and cache budgets are per instance.

The value is immutable after creation. Changing it forces replacement of the
resource, including when reverting from `1` to the default. Existing resources
continue to use `2`. Set the matching value when importing a single instance.
This provider does not calculate a reduced price for single-instance mode.

Requires a platform implementing cetic-cloud-platform#1748. Upgrade the
platform before requesting the new mode. The provider can be installed first
while keeping the historical value `2`. An older platform may ignore the
field: if the returned mode differs, creation fails explicitly and preserves
the created ID in Terraform state so the resource is not orphaned.
