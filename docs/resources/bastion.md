---
page_title: "ccp_bastion Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a secure SSH access bastion on CETIC Cloud Platform.
---

# ccp_bastion (Resource)

Manages a **bastion** — a managed secure SSH access appliance that fronts the private instances of one or more VPCs. Instead of exposing every instance to the public internet, operators reach their otherwise-unreachable private hosts (containers, VMs, …) through a single, audited entry point. The bastion exposes one public SSH endpoint (`endpoint_host:endpoint_port`); connections are routed to the private instances of the bastion's VPCs.

~> **Note:** Every settable attribute (`name`, `region`, `plan`, `vpc_id`, `vpc_ids`, `public_ip_id`, `tags`) is immutable after creation. To change any of them, create a new `ccp_bastion` resource and delete the old one. The CETIC Cloud API has no update endpoint for bastions.

~> **Note:** Provisioning is asynchronous. Right after `terraform apply`, `status` is `provisioning` and `endpoint_host` / `endpoint_port` / `public_ip_address` may be empty; they are populated once the appliance becomes `active`. A subsequent `terraform refresh` reflects the final endpoint.

## Example Usage

### Minimal (single VPC, defaults)

```hcl
resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

# Secure SSH entry point for the private instances of the VPC.
resource "ccp_bastion" "ops" {
  name   = "ops-bastion"
  region = "RNN"
  vpc_id = ccp_vpc.prod.id
}

output "bastion_ssh" {
  value = "${ccp_bastion.ops.endpoint_host}:${ccp_bastion.ops.endpoint_port}"
}
```

### Multi-VPC bastion with an explicit plan, public IP and tags

```hcl
resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

resource "ccp_vpc" "staging" {
  name   = "staging"
  region = "RNN"
}

# Optional: bring your own reserved public IP (otherwise IPaaS allocates one).
resource "ccp_public_ip" "bastion" {
  region = "RNN"
}

resource "ccp_bastion" "ops" {
  name   = "ops-bastion"
  region = "RNN"
  plan   = "medium" # small | medium | large (default: small)

  # vpc_id is the primary VPC; vpc_ids covers all fronted VPCs (1–5).
  vpc_id  = ccp_vpc.prod.id
  vpc_ids = [ccp_vpc.prod.id, ccp_vpc.staging.id]

  public_ip_id = ccp_public_ip.bastion.id

  tags = ["ops", "remote-access"]
}

output "bastion_ssh" {
  value = "${ccp_bastion.ops.endpoint_host}:${ccp_bastion.ops.endpoint_port}"
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Human-readable name for the bastion (max 100 chars; letters, digits, `_`, `-`, and spaces). Shown in the console and CLI.
- `region` - (Required, Forces new resource) Region code the bastion is provisioned in (e.g. `RNN`).
- `vpc_id` - (Required, Forces new resource) UUID of the primary VPC whose private instances the bastion grants SSH access to. This VPC is always part of the bastion's VPC set.

### Optional

- `plan` - (Optional, Computed, Forces new resource) Sizing plan: `small`, `medium`, or `large`. Defaults to `small`.
- `vpc_ids` - (Optional, Computed, Forces new resource) UUIDs of all the VPCs the bastion fronts (1–5). The primary `vpc_id` is always included — list it explicitly to control ordering, or add only the extra VPCs. If omitted, the bastion covers just `vpc_id`.
- `public_ip_id` - (Optional, Computed, Forces new resource) UUID of a reserved public IP to attach to the bastion endpoint. If omitted, the platform allocates one (IPaaS).
- `tags` - (Optional, Computed, Forces new resource) Free-form labels attached to the bastion.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the bastion.
- `status` - Lifecycle status: `provisioning`, `active`, `error`, or `deleting`. Read-only and volatile.
- `endpoint_host` - Public SSH endpoint hostname (or IP) clients connect to. Populated once the appliance finishes provisioning.
- `endpoint_port` - TCP port of the SSH endpoint. Populated once the appliance finishes provisioning.
- `public_ip_address` - Public IP address attached to the bastion endpoint. Populated once the appliance finishes provisioning.

## Import

Bastions can be imported using their UUID:

```
terraform import ccp_bastion.ops <bastion_id>
```
