terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 2.0"
    }
  }
}

provider "ccp" {
  # api_key et api_url depuis env CCP_API_KEY / CCP_API_URL
}

# ─── Pré-requis réseau ──────────────────────────────────────────────────────

resource "ccp_vpc" "demo" {
  name   = "appgw-demo-vpc"
  region = "RNN"
}

resource "ccp_vnet" "web" {
  vpc_id = ccp_vpc.demo.id
  name   = "web-tier"
  cidr   = "10.42.1.0/24"
  snat   = true
}

resource "ccp_public_ip" "appgw" {
  region = "RNN"
}

# ─── Backends ──────────────────────────────────────────────────────────────

resource "ccp_container_instance" "api_01" {
  name     = "api-01"
  region   = "RNN"
  plan     = "small"
  template = "ubuntu-24.04"
  vnet_id  = ccp_vnet.web.id
  tags     = ["api", "env:prod"]
}

resource "ccp_container_instance" "api_02" {
  name     = "api-02"
  region   = "RNN"
  plan     = "small"
  template = "ubuntu-24.04"
  vnet_id  = ccp_vnet.web.id
  tags     = ["api", "env:prod"]
}

# ─── Application Gateway ───────────────────────────────────────────────────

resource "ccp_application_gateway" "web" {
  name         = "web-appgw"
  region       = "RNN"
  plan         = "medium"
  vpc_id       = ccp_vpc.demo.id
  vnet_id      = ccp_vnet.web.id
  public_ip_id = ccp_public_ip.appgw.id

  force_https               = true
  hsts_enabled              = true
  hsts_max_age              = 31536000
  global_rate_limit_per_sec = 1000

  tags = ["env:prod", "team:web"]
}

resource "ccp_appgw_listener" "api" {
  appgw_id = ccp_application_gateway.web.id
  hostname = "api.example.com"
}

resource "ccp_appgw_target_group" "api_pool" {
  appgw_id  = ccp_application_gateway.web.id
  name      = "api-pool"
  algorithm = "leastconn"
  hc_path   = "/healthz"
}

resource "ccp_appgw_target_group_member" "api_01" {
  appgw_id        = ccp_application_gateway.web.id
  target_group_id = ccp_appgw_target_group.api_pool.id
  container_id    = ccp_container_instance.api_01.id
  port            = 8080
}

resource "ccp_appgw_target_group_member" "api_02" {
  appgw_id        = ccp_application_gateway.web.id
  target_group_id = ccp_appgw_target_group.api_pool.id
  container_id    = ccp_container_instance.api_02.id
  port            = 8080
}

resource "ccp_appgw_route" "api_v1" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.api.id
  priority        = 10
  path_match      = "/v1/"
  path_match_type = "prefix"
  target_group_id = ccp_appgw_target_group.api_pool.id

  rate_limit_per_sec = 50
  cors_enabled       = true
  cors_origins       = ["https://app.example.com"]
  waf_preset         = "permissive"
}

# Admin route guarded by HTTP Basic auth. Plaintext passwords are bcrypt-
# hashed server-side into an encrypted Secret Manager entry referenced
# by `basic_auth_secret_ref` (read-only, Sensitive).
variable "admin_password" {
  type      = string
  sensitive = true
}

resource "ccp_appgw_route" "admin" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.api.id
  priority        = 5
  path_match      = "/admin/"
  path_match_type = "prefix"
  target_group_id = ccp_appgw_target_group.api_pool.id

  rate_limit_per_sec = 5
  allow_cidrs        = ["10.0.0.0/8"]
  waf_preset         = "strict"

  basic_auth_user {
    user     = "admin"
    password = var.admin_password
  }
}

# ─── Outputs ───────────────────────────────────────────────────────────────

output "appgw_vip" {
  value = ccp_application_gateway.web.vip_address
}

output "appgw_public_ip" {
  value = ccp_public_ip.appgw.ip_address
}

# `public_ip_status` walks `allocated → attaching → attached` while the
# IPaaS pipeline converges; the provider blocks on it during apply.
output "appgw_public_ip_status" {
  value = ccp_application_gateway.web.public_ip_status
}

output "appgw_admin_basic_auth_secret_ref" {
  value     = ccp_appgw_route.admin.basic_auth_secret_ref
  sensitive = true
}
