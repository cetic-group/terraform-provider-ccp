terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 4.7"
    }
  }
}

provider "ccp" {
  # api_key depuis env CCP_API_KEY
}

# ─── Network pre-requisites ──────────────────────────────────────────────────

resource "ccp_vpc" "demo" {
  name   = "demo-vpc"
  region = "RNN"
}

resource "ccp_vnet" "front" {
  vpc_id = ccp_vpc.demo.id
  name   = "front"
  cidr   = "10.42.0.0/24"
  snat   = true
}

# ─── Backend container ───────────────────────────────────────────────────────

resource "ccp_container_instance" "web" {
  name     = "web-1"
  region   = "RNN"
  plan     = "small"
  template = "ubuntu-24.04"
  vnet_id  = ccp_vnet.front.id
}

# ─── Public IP (optional, attach to LB) ─────────────────────────────────────

resource "ccp_public_ip" "lb" {
  region = "RNN"
}

# ─── Load Balancer with HTTPS + Let's Encrypt (HTTP-01 challenge) ─────────────

resource "ccp_load_balancer" "web" {
  name    = "web-lb"
  region  = "RNN"
  vnet_id = ccp_vnet.front.id
  plan    = "small" # small (default) | medium | large

  public_ip_id = ccp_public_ip.lb.id

  # HTTPS listener with automatic Let's Encrypt certificate (HTTP-01 challenge).
  # The load balancer must be reachable on port 80 for the domain to pass
  # the challenge — typically achieved by also adding the http listener below.
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

  # HTTP listener (plain traffic and Let's Encrypt HTTP-01 challenge path)
  listener {
    protocol    = "http"
    listen_port = 80

    backend {
      container_id = ccp_container_instance.web.id
      port         = 8080
    }
  }
}

# ─── Outputs ─────────────────────────────────────────────────────────────────

output "lb_vip" {
  description = "Private VIP of the LB on the VNet"
  value       = ccp_load_balancer.web.vip_address
}

output "lb_public_ip" {
  description = "Public IP address attached to the LB"
  value       = ccp_load_balancer.web.public_ip_address
}
