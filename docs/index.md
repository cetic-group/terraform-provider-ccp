---
page_title: "Provider: CETIC Cloud Platform"
description: |-
  The CETIC Cloud Platform (CCP) provider manages infrastructure on CETIC Cloud — containers, VMs, Kubernetes, databases, networking, and storage.
---

# CETIC Cloud Platform Provider

The **CETIC Cloud Platform** (CCP) provider lets you manage infrastructure resources on [CETIC Cloud](https://console.cloud.cetic-group.com) — a sovereign cloud offering containers, virtual machines, Kubernetes as a Service, managed databases (PostgreSQL, Valkey, MariaDB, FerretDB), block and object storage, load balancers, and advanced VPC networking.

## Provider name

The provider source is `cetic-group/cetic-cloud-platform`. We recommend
declaring it with the **local name `ccp`** (shorter, used throughout this
doc and in all examples):

```hcl
terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 0.18.0"
    }
  }
}
```

After this declaration you reference it as `provider "ccp"` and resources
as `ccp_vpc`, `ccp_container_instance`, etc. (the resource prefix `ccp_`
is fixed by the provider — independent of the local name you chose).

If you skip the alias and use the default name (the last segment of the
source path), you'd write `provider "cetic-cloud-platform"`. Both work
but **all examples in this documentation use `ccp`** — copy the
`required_providers` block above to keep them as-is.

## Authentication

The provider authenticates using an API key. Generate one from the CETIC Cloud console at **Settings → API Keys**.

Set the key via environment variable (recommended — never commit credentials to source control):

```shell
export CCP_API_KEY="ccp_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

Or pass it directly in the provider block (use a variable, not a literal):

```hcl
provider "ccp" {
  api_key = var.ccp_api_key
}
```

## Full provider block

```hcl
terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 0.18.0"
    }
  }
}

provider "ccp" {
  api_key  = var.ccp_api_key                # or env CCP_API_KEY
  endpoint = "https://api.cloud.cetic-group.com"  # optional, env CCP_API_URL
}

variable "ccp_api_key" {
  description = "CETIC Cloud API key"
  type        = string
  sensitive   = true
}
```

## Configuration Reference

| Argument | Environment Variable | Required | Description |
|---|---|---|---|
| `api_key` | `CCP_API_KEY` | Yes | API key prefixed `ccp_live_`. Generate in console → Settings → API Keys. |
| `endpoint` | `CCP_API_URL` | No | Override the API base URL. Defaults to `https://api.cloud.cetic-group.com`. |

## Example: Full Stack

The following example creates an SSH key, a VPC with two subnets, a container, and a VM with a public IP. Assumes you've declared the `required_providers` block above (otherwise replace `ccp` with `cetic-cloud-platform`).

```hcl
# Suppose `terraform { required_providers { ccp = { ... } } }` already declared.
provider "ccp" {
  api_key = var.ccp_api_key
}

# Identity

resource "ccp_ssh_key" "ops" {
  name       = "ops-team"
  public_key = file("~/.ssh/id_ed25519.pub")
}

# Network

resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
  tags   = ["env:prod"]
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

# Public IP for the web container

resource "ccp_public_ip" "web" {
  region = "RNN"
}

# Compute

resource "ccp_container_instance" "web" {
  name        = "web-01"
  region      = "RNN"
  plan        = "small"
  template    = "ubuntu-24.04"
  vnet_id     = ccp_vnet.web.id
  ssh_key_ids = [ccp_ssh_key.ops.id]
  tags        = ["web", "env:prod"]

  user_data = <<-EOF
    #!/bin/bash
    apt-get update -q && apt-get install -y -q nginx
    systemctl enable --now nginx
  EOF
}

resource "ccp_vm_instance" "app" {
  name        = "app-server"
  region      = "RNN"
  plan        = "medium"
  template    = "ubuntu-24.04"
  vnet_id     = ccp_vnet.web.id
  ssh_key_ids = [ccp_ssh_key.ops.id]
  tags        = ["app", "env:prod"]
}

# Outputs

output "web_public_ip" {
  value = ccp_public_ip.web.ip_address
}

output "app_private_ip" {
  value = ccp_vm_instance.app.ip_address
}
```

## Regions

| ID | Name | Location | Timezone |
|---|---|---|---|
| `RNN` | Rennes | Rennes, France | Europe/Paris |
| `PAR` | Paris | Paris, France | Europe/Paris |
| `ABJ` | Abidjan | Abidjan, Côte d'Ivoire | Africa/Abidjan |

## Resource Plans

All compute resources (containers, VMs, Kubernetes nodes) use the following plans:

| Plan | vCPU | RAM | SSD |
|---|---|---|---|
| `nano` | 1 | 512 MB | 10 GB |
| `micro` | 1 | 1 GB | 20 GB |
| `small` | 2 | 2 GB | 40 GB |
| `medium` | 4 | 8 GB | 80 GB |
| `large` | 8 | 16 GB | 160 GB |
| `xlarge` | 16 | 32 GB | 320 GB |

Billing is pay-as-you-go, charged by the hour. Egress bandwidth is free.

## Documentation

Full platform documentation is available at [docs.cloud.cetic-group.com](https://docs.cloud.cetic-group.com).
