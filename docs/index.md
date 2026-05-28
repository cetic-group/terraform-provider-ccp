---
page_title: "Provider: CETIC Cloud Platform"
description: |-
  The CETIC Cloud Platform (CCP) provider manages infrastructure on CETIC Cloud — containers, VMs, Kubernetes, databases, networking, and storage.
---

# CETIC Cloud Platform Provider

The **CETIC Cloud Platform** (CCP) provider lets you manage infrastructure resources on [CETIC Cloud](https://console.cloud.cetic-group.com) — a sovereign cloud offering containers, virtual machines, Kubernetes as a Service, managed databases (PostgreSQL, Valkey, MariaDB, FerretDB), block and object storage, load balancers, and advanced VPC networking.

## Quick start

Declare the provider with the canonical local name `cetic-cloud-platform`
(this matches the Terraform Registry's "Use Provider" snippet). The
resource type prefix `ccp_` is independent of the provider's local name —
that's why every `resource` / `data` block in the new style explicitly
references `provider = cetic-cloud-platform`.

```hcl
terraform {
  required_providers {
    cetic-cloud-platform = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 1.0"
    }
  }
}

provider "cetic-cloud-platform" {
  api_key = var.ccp_api_key   # or env CCP_API_KEY
}

resource "ccp_vpc" "main" {
  provider = cetic-cloud-platform
  name     = "production"
  region   = "RNN"
}
```

### Backward-compatible alias

Older configurations declared the provider with the local name `ccp`, which
also matched the resource prefix and avoided the per-resource `provider =`
attribute. **This still works** — the provider exports the same resource
type names regardless of the local alias used:

```hcl
terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 1.0"
    }
  }
}

provider "ccp" {
  api_key = var.ccp_api_key
}

resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
}
```

If you prefer this terser form (no explicit `provider =` on every
resource), use the `ccp` local name. The two styles are functionally
equivalent; pick whichever you find more readable.

## Authentication

The provider authenticates using an API key. Generate one from the CETIC Cloud console at **Settings → API Keys**.

Set the key via environment variable (recommended — never commit credentials to source control):

```shell
export CCP_API_KEY="ccp_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

Or pass it directly in the provider block (use a variable, not a literal):

```hcl
provider "cetic-cloud-platform" {
  api_key = var.ccp_api_key
}
```

## Full provider block

```hcl
terraform {
  required_providers {
    cetic-cloud-platform = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 1.0"
    }
  }
}

provider "cetic-cloud-platform" {
  api_key  = var.ccp_api_key                       # or env CCP_API_KEY
  endpoint = "https://api.cloud.cetic-group.com"   # optional, env CCP_API_URL
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

## Example: full stack

The example below creates an SSH key, a VPC with two subnets, a container, and a VM with a public IP. The provider is declared with local name `cetic-cloud-platform`, so every resource block sets `provider = cetic-cloud-platform`.

```hcl
terraform {
  required_providers {
    cetic-cloud-platform = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 1.0"
    }
  }
}

provider "cetic-cloud-platform" {
  api_key = var.ccp_api_key
}

# Identity

resource "ccp_ssh_key" "ops" {
  provider   = cetic-cloud-platform
  name       = "ops-team"
  public_key = file("~/.ssh/id_ed25519.pub")
}

# Network

resource "ccp_vpc" "main" {
  provider = cetic-cloud-platform
  name     = "production"
  region   = "RNN"
  tags     = ["env:prod"]
}

resource "ccp_vnet" "web" {
  provider = cetic-cloud-platform
  vpc_id   = ccp_vpc.main.id
  name     = "web-tier"
  cidr     = "10.0.1.0/24"
  snat     = true
}

resource "ccp_vnet" "data" {
  provider = cetic-cloud-platform
  vpc_id   = ccp_vpc.main.id
  name     = "data-tier"
  cidr     = "10.0.2.0/24"
  snat     = true
}

# Public IP for the web container

resource "ccp_public_ip" "web" {
  provider = cetic-cloud-platform
  region   = "RNN"
}

# Compute

resource "ccp_container_instance" "web" {
  provider    = cetic-cloud-platform
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
  provider    = cetic-cloud-platform
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

The same example with the `ccp` local-name alias (no per-resource
`provider =` lines) is equally valid — swap the `required_providers` and
`provider` blocks for their `ccp` variants and remove the
`provider = cetic-cloud-platform` lines.

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
