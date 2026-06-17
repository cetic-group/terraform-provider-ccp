# CETIC Cloud Platform — basic example
#
# Prerequisites:
#   export CCP_API_KEY="ccp_live_..."   (required, scope >= write)
#
# Note: ccp_vpc and ccp_vnet are provisioned asynchronously by
# the CCP SDN backend. The provider polls the resource until it
# reaches "active" — expect up to ~90s on first apply.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 5.0"
    }
  }
}

provider "ccp" {
  api_key = "ccp_live_..." # or via CCP_API_KEY env var
}

# ---------------------------------------------------------------------------
# Data source — list available regions
# ---------------------------------------------------------------------------

data "ccp_regions" "available" {}

output "regions" {
  value = data.ccp_regions.available.regions
}

# ---------------------------------------------------------------------------
# SSH key — synchronous, no Update (any change forces replacement)
# ---------------------------------------------------------------------------

resource "ccp_ssh_key" "admin" {
  name       = "admin-laptop"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample admin@laptop"
}

# ---------------------------------------------------------------------------
# VPC — async create with status polling
# ---------------------------------------------------------------------------

resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
  cidr   = "10.0.0.0/16" # omit to let the platform auto-allocate a /16
  tags   = ["env:prod", "tier:public"]
}

# ---------------------------------------------------------------------------
# VNets — nested under VPC, supports PATCH on name + snat
# ---------------------------------------------------------------------------

resource "ccp_vnet" "web" {
  vpc_id = ccp_vpc.prod.id
  name   = "web"
  cidr   = "10.0.1.0/24"
  snat   = true
  tags   = ["zone:web"]
}

resource "ccp_vnet" "db" {
  vpc_id = ccp_vpc.prod.id
  name   = "db"
  cidr   = "10.0.2.0/24"
  snat   = false # air-gapped
}

# ---------------------------------------------------------------------------
# Public IP — allocated, attached to the container below
# ---------------------------------------------------------------------------

resource "ccp_public_ip" "web" {
  region           = "RNN"
  attached_to_id   = ccp_container_instance.web.id
  attached_to_type = "container"
}

# ---------------------------------------------------------------------------
# Container instance — LXC, attached to the web VNet
# ---------------------------------------------------------------------------

resource "ccp_container_instance" "web" {
  name     = "web-1"
  region   = "RNN"
  plan     = "small"
  template = "ubuntu-24.04"
  vnet_id  = ccp_vnet.web.id

  ssh_key_ids = [ccp_ssh_key.admin.id]

  tags = ["app:web", "env:prod"]

  user_data = <<-EOT
    #cloud-config
    package_update: true
    packages:
      - nginx
    runcmd:
      - systemctl enable --now nginx
  EOT
}

# ---------------------------------------------------------------------------
# Block volume — Ceph RBD, attached to the container, 20 GB
# ---------------------------------------------------------------------------

resource "ccp_block_volume" "data" {
  name    = "web-data"
  region  = "RNN"
  size_gb = 20

  attached_to_id   = ccp_container_instance.web.id
  attached_to_type = "container"

  tags = ["app:web", "purpose:data"]
}

# ---------------------------------------------------------------------------
# Outputs
# ---------------------------------------------------------------------------

output "vpc_status" {
  value = ccp_vpc.prod.status
}

output "vnet_web_gateway" {
  value = ccp_vnet.web.gateway
}

output "container_private_ip" {
  value = ccp_container_instance.web.ip_address
}

output "container_public_ip" {
  value = ccp_public_ip.web.ip_address
}

output "volume_status" {
  value = ccp_block_volume.data.status
}
