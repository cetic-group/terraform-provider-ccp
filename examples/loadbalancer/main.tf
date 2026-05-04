terraform {
  required_providers {
    cloudlake = {
      source = "cetic-group/cloudlake"
    }
  }
}

provider "cloudlake" {
  # api_key et api_url depuis env CCP_API_KEY / CL_API_URL ou ~/.cloudlake/config.yaml
}

# Pré-requis : un VPC et un VNet existants
resource "ccp_vpc" "demo" {
  name   = "demo-vpc"
  region = "RNN"
}

resource "ccp_vnet" "demo" {
  vpc_id = ccp_vpc.demo.id
  name   = "demo-vnet"
  cidr   = "10.42.0.0/24"
  snat   = true
}

# Allocation d'une IP publique optionnelle (à attacher au LB)
resource "ccp_public_ip" "lb" {
  region = "RNN"
}

# Le Load Balancer lui-même
resource "ccp_load_balancer" "demo" {
  name         = "demo-lb"
  region       = "RNN"
  vnet_id      = ccp_vnet.demo.id
  public_ip_id = ccp_public_ip.lb.id
  tags         = ["demo", "terraform-managed"]
}

output "lb_vip" {
  description = "VIP privée du LB sur le VNet"
  value       = cloudlake_load_balancer.demo.vip_address
}

output "lb_public_ip" {
  description = "IP publique attachée"
  value       = cloudlake_load_balancer.demo.public_ip_address
}
