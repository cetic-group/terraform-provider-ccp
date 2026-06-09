---
page_title: "ccp_bastion Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a secure SSH access bastion on CETIC Cloud Platform.
---

# ccp_bastion (Resource)

Manages a **bastion** — a managed secure SSH access appliance that fronts the private instances of a VPC. Instead of exposing every instance to the public internet, operators reach their otherwise-unreachable private hosts (containers, VMs, …) through a single, audited entry point. The bastion exposes one public SSH endpoint (`endpoint_host:endpoint_port`); connections are routed to the private instances of the bastion's VPC.

~> **Note:** `name`, `region` and `vpc_id` are all immutable after creation. To move a bastion to another VPC or region, create a new `ccp_bastion` resource and delete the old one. The CETIC Cloud API has no update endpoint for bastions.

~> **Note:** Provisioning is asynchronous. Right after `terraform apply`, `status` is `provisioning` and `endpoint_host` / `endpoint_port` may be empty; they are populated once the appliance becomes `active`. A subsequent `terraform refresh` reflects the final endpoint.

## Example Usage

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

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Human-readable name for the bastion (max 100 chars; letters, digits, `_`, `-`, and spaces). Shown in the console and CLI.
- `region` - (Required, Forces new resource) Region code the bastion is provisioned in (e.g. `RNN`).
- `vpc_id` - (Required, Forces new resource) UUID of the VPC whose private instances the bastion grants SSH access to.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the bastion.
- `status` - Lifecycle status: `provisioning`, `active`, `error`, or `deleting`. Read-only and volatile.
- `endpoint_host` - Public SSH endpoint hostname (or IP) clients connect to. Populated once the appliance finishes provisioning.
- `endpoint_port` - TCP port of the SSH endpoint. Populated once the appliance finishes provisioning.

## Import

Bastions can be imported using their UUID:

```
terraform import ccp_bastion.ops <bastion_id>
```
