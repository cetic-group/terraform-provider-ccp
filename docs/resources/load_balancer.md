---
page_title: "ccp_load_balancer Resource - cetic-cloud-platform"
subcategory: "Compute"
description: |-
  Manages a load balancer on CETIC Cloud Platform.
---

# ccp_load_balancer (Resource)

Manages a load balancer on CETIC Cloud Platform. Each load balancer is provisioned as a highly-available pair of instances running HAProxy with a floating virtual IP managed by Keepalived VRRP. Inter-node HA is provided via VXLAN zone networking, enabling failover across Proxmox nodes without downtime.

~> **Note:** Load balancer provisioning is asynchronous and includes OS installation, HAProxy/Keepalived configuration, and health-check validation. The provider polls until the load balancer reaches `active` status, which typically takes 3 to 5 minutes.

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
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the load balancer.
- `region` - (Required, Forces new resource) Region where the load balancer is created. One of: `RNN`, `PAR`, `ABJ`.
- `vnet_id` - (Required, Forces new resource) UUID of the VNet where the load balancer's virtual IP (VIP) will be hosted. Backend instances must be accessible from this VNet.

### Optional

- `public_ip_id` - (Optional) UUID of a public IP to attach to the load balancer. The public IP must be in the same region. When set, inbound traffic to this IP is forwarded to the VIP.
- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the load balancer.
- `status` - Current status. Possible values: `provisioning`, `active`, `error`.
- `vip_address` - Private virtual IP address of the load balancer within the VNet.
- `public_ip_address` - Public IP address if one is attached, otherwise empty.

## Import

Load balancers can be imported using their UUID:

```
terraform import ccp_load_balancer.web <lb_id>
```
