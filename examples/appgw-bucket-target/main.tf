# Serve a public bucket under your own domain through an Application Gateway.
#
# After `terraform apply`, create ONE DNS record at your DNS host pointing
# `assets.example.com` to the gateway's public IP (output below). No server, key,
# IP address or TLS option is needed on your side: the gateway issues the
# certificate itself.
#
# A bucket target is read-only (GET and HEAD) and does not list the content of
# the bucket.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 7.4"
    }
  }
}

provider "ccp" {
  # api_key from the CCP_API_KEY environment variable
}

variable "vpc_id" { type = string }
variable "vnet_id" { type = string }

# The bucket must be public and in the same region as the gateway.
resource "ccp_object_bucket" "site" {
  name      = "acme-public-site"
  region    = "RNN"
  is_public = true
}

resource "ccp_public_ip" "gateway" {
  region = "RNN"
}

resource "ccp_application_gateway" "web" {
  name         = "site-gateway"
  region       = "RNN"
  plan         = "appgw-small"
  vpc_id       = var.vpc_id
  vnet_id      = var.vnet_id
  public_ip_id = ccp_public_ip.gateway.id

  # Optional: lets routes cache what they serve (see `cache_mode` below).
  cache_enabled = true
}

# The target group: the bucket, and optionally one folder of it.
resource "ccp_appgw_target_group" "site" {
  appgw_id      = ccp_application_gateway.web.id
  name          = "site-assets"
  kind          = "bucket"
  bucket_id     = ccp_object_bucket.site.id
  bucket_prefix = "site/public" # optional; omit to serve the whole bucket
}

resource "ccp_appgw_listener" "assets" {
  appgw_id       = ccp_application_gateway.web.id
  hostname       = "assets.example.com"
  acme_challenge = "http01"
}

resource "ccp_appgw_route" "assets" {
  appgw_id        = ccp_application_gateway.web.id
  listener_id     = ccp_appgw_listener.assets.id
  target_group_id = ccp_appgw_target_group.site.id

  # Caching is a property of the route, not of the target group.
  # "Stable content" (images, files): keep responses for up to an hour.
  cache_mode          = "force"
  cache_default_ttl_s = 3600
  cache_max_ttl_s     = 3600
}

output "dns_target" {
  description = "Point a DNS record for assets.example.com to this address."
  value       = ccp_public_ip.gateway.ip_address
}

output "origin_status" {
  description = "ok when the bucket is served; otherwise see origin_error."
  value       = ccp_appgw_target_group.site.origin_status
}

output "origin_error" {
  value = ccp_appgw_target_group.site.origin_error
}
