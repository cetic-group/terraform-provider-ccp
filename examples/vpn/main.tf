terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 4.5"
    }
  }
}

provider "ccp" {
  # api_key depuis env CCP_API_KEY
}

# ─── Network pre-requisite ────────────────────────────────────────────────────

resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

# ─── Optional: bring your own reserved public IP ─────────────────────────────
# Omit this block (and public_ip_id below) to let IPaaS allocate one.

resource "ccp_public_ip" "vpn" {
  region = "RNN"
}

# ─── WireGuard VPN gateway ────────────────────────────────────────────────────

resource "ccp_vpn_gateway" "ops" {
  name   = "ops-vpn"
  region = "RNN"
  plan   = "small" # small | medium | large

  vpc_ids = [ccp_vpc.prod.id]

  public_ip_id   = ccp_public_ip.vpn.id
  peer_pool_cidr = "10.99.0.0/24"
  dns            = "10.0.0.2"

  tags = ["ops", "remote-access"]
}

# ─── Peer A: server generates the keypair (Model B) ──────────────────────────
# `config` contains the private key — treat it as a secret.

resource "ccp_vpn_peer" "laptop" {
  gateway_id        = ccp_vpn_gateway.ops.id
  name              = "alice-laptop"
  store_private_key = true
  one_time          = false
}

# ─── Peer B: bring your own key (Model A) ─────────────────────────────────────
# The platform never sees a private key; `config` has no PrivateKey line.

resource "ccp_vpn_peer" "router" {
  gateway_id = ccp_vpn_gateway.ops.id
  name       = "branch-router"
  public_key = "REPLACE_WITH_YOUR_WIREGUARD_PUBLIC_KEY="
}

# ─── Outputs ─────────────────────────────────────────────────────────────────

output "vpn_endpoint" {
  description = "Public WireGuard endpoint of the gateway"
  value       = "${ccp_vpn_gateway.ops.endpoint_host}:${ccp_vpn_gateway.ops.endpoint_port}"
}

output "vpn_public_key" {
  description = "Gateway WireGuard public key (referenced by each peer config)"
  value       = ccp_vpn_gateway.ops.public_key
}

output "laptop_wireguard_config" {
  description = "Ready-to-use client config for the laptop peer (contains its private key)"
  value       = ccp_vpn_peer.laptop.config
  sensitive   = true
}
