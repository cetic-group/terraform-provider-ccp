---
page_title: "ccp_vpc Resource - cetic-cloud-platform"
subcategory: "Networking"
description: |-
  Manages a VPC (Virtual Private Cloud) on CETIC Cloud Platform.
---

# ccp_vpc (Resource)

Manages a VPC (Virtual Private Cloud) on CETIC Cloud Platform. Each VPC creates an isolated private network in the target region. A NAT Gateway is automatically provisioned when the first VNet is created inside the VPC, providing internet access for all instances without exposing private IPs.

~> **Note:** VPC creation is asynchronous — the provider polls until the VPC reaches `active` status. Provisioning typically completes within 30 seconds.

## Example Usage

```hcl
resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
  tags   = ["env:prod", "team:infra"]
}

resource "ccp_vnet" "web" {
  vpc_id = ccp_vpc.main.id
  name   = "web-tier"
  cidr   = "10.0.1.0/24"
  snat   = true
}

resource "ccp_vnet" "data" {
  vpc_id = ccp_vpc.main.id
  name   = "data-tier"
  cidr   = "10.0.2.0/24"
  snat   = true
}
```

## Argument Reference

### Required

- `name` - (Required) Name of the VPC. Must be unique within the region.
- `region` - (Required, Forces new resource) Region where the VPC is created. One of: `RNN` (Rennes), `PAR` (Paris), `ABJ` (Abidjan).

### Optional

- `tags` - (Optional) List of free-form tags (max 60, max 50 chars each). Example: `["env:prod", "team:infra"]`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the VPC.
- `status` - Current status of the VPC. Possible values: `pending`, `active`, `error`.
- `created_at` - Timestamp of creation (RFC3339).

## Import

VPCs can be imported using their UUID:

```
terraform import ccp_vpc.main <vpc_id>
```
