---
page_title: "ccp_load_balancer Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a load balancer on CETIC Cloud Platform.
---

# ccp_load_balancer (Resource)

Manages a load balancer on CETIC Cloud Platform. Each load balancer is highly available, with a floating virtual IP and automatic failover across availability zones — no downtime during node failures.

Listeners and their backends are declared as nested blocks and are fully reconciled on every apply.

~> **Note:** Load balancer provisioning is asynchronous. The provider polls until the load balancer reaches `active` status, which typically takes 3 to 5 minutes.

## Example Usage

```hcl
resource "ccp_public_ip" "web_lb" {
  region = "RNN"
}

resource "ccp_load_balancer" "web" {
  name         = "web-lb"
  region       = "RNN"
  vnet_id      = ccp_vnet.web.id
  public_ip_id = ccp_public_ip.web_lb.id
  tags         = ["web", "env:prod"]

  listener {
    name          = "http"
    algorithm     = "round_robin"
    protocol      = "http"
    frontend_port = 80

    backend {
      container_id = ccp_container_instance.web_01.id
      port         = 8080
      weight       = 1
    }

    backend {
      container_id = ccp_container_instance.web_02.id
      port         = 8080
      weight       = 1
    }
  }

  listener {
    name          = "https"
    algorithm     = "round_robin"
    protocol      = "tcp"
    frontend_port = 443

    backend {
      container_id = ccp_container_instance.web_01.id
      port         = 8443
    }

    backend {
      container_id = ccp_container_instance.web_02.id
      port         = 8443
    }
  }
}

output "lb_public_ip" {
  value = ccp_public_ip.web_lb.ip_address
}

output "lb_vip" {
  value = ccp_load_balancer.web.vip_address
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the load balancer.
- `region` - (Required, Forces new resource) Region where the load balancer is created. One of: `RNN`, `PAR`, `ABJ`.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the load balancer's virtual IP (VIP) will be hosted. Backend instances must be reachable from this VNet.

### Optional

- `public_ip_id` - (Optional) UUID of a public IP to attach to the load balancer. The public IP must be in the same region. Remove to detach.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).
- `listener` - (Optional) One or more listener blocks (see [Listener Reference](#listener-reference) below).

## Listener Reference

Each `listener` block supports:

### Required

- `name` - (Required) Listener name (1-100 chars). Used as the stable key for reconciliation across applies.
- `algorithm` - (Required) Load-balancing algorithm. One of: `round_robin`, `least_conn`, `ip_hash`.
- `protocol` - (Required) Protocol. One of: `tcp`, `http`.
- `frontend_port` - (Required) Port the load balancer listens on (1-65535).

### Optional

- `backend` - (Optional) One or more backend blocks (see [Backend Reference](#backend-reference) below).

### Computed

- `id` - The UUID of the listener.

## Backend Reference

Each `backend` block inside a `listener` supports:

### Required (one of)

- `container_id` - (Optional) UUID of a container instance to route traffic to. Exactly one of `container_id` or `vm_instance_id` must be set.
- `vm_instance_id` - (Optional) UUID of a VM instance to route traffic to.

### Required

- `port` - (Required) Backend port (1-65535). Must match the port the instance's service listens on.

### Optional

- `weight` - (Optional) Backend weight for weighted round-robin (1-100). Defaults to `1`.

### Computed

- `id` - The UUID of the backend.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the load balancer.
- `status` - Current status: `provisioning`, `active`, `updating`, or `error`.
- `vip_address` - Private virtual IP address within the VNet.
- `public_ip_address` - Public IP address if one is attached, otherwise empty.

## Import

Load balancers can be imported using their UUID:

```
terraform import ccp_load_balancer.web <lb_id>
```

~> **Note:** After import, listeners and backends are read from the API state. Subsequent `terraform plan` will show the current listener/backend configuration.
