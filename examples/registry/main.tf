# End-to-end example for ccp_registry — VPC + VNet + public IP + registry
# + 2 users (admin/ci-pull) + 2 ACLs.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 4.7"
    }
  }
}

provider "ccp" {}

# ─── Networking ────────────────────────────────────────────────────────────

resource "ccp_vpc" "main" {
  name   = "registry-demo"
  region = "RNN"
  tags   = ["env:demo", "service:ccr"]
}

resource "ccp_vnet" "registry" {
  vpc_id = ccp_vpc.main.id
  name   = "registry-tier"
  cidr   = "10.10.0.0/24"
  snat   = true
}

# Public IP for the public exposure.
resource "ccp_public_ip" "registry" {
  region = "RNN"
}

# ─── Registry (public exposure) ────────────────────────────────────────────

resource "ccp_registry" "main" {
  name             = "ccr-demo"
  region           = "RNN"
  vpc_id           = ccp_vpc.main.id
  vnet_id          = ccp_vnet.registry.id
  exposure         = "public"
  public_ip_id     = ccp_public_ip.registry.id
  gc_schedule_cron = "0 3 * * 0" # Sunday 03:00 UTC
  tags             = ["env:demo"]
}

# ─── Users ─────────────────────────────────────────────────────────────────

# Human admin user — full access.
resource "ccp_registry_user" "alice" {
  registry_id = ccp_registry.main.id
  username    = "alice"
}

# CI/CD pipeline — push to a single namespace, no admin.
resource "ccp_registry_user" "ci_pull" {
  registry_id = ccp_registry.main.id
  username    = "ci-pull"
}

# ─── ACLs ──────────────────────────────────────────────────────────────────

resource "ccp_registry_acl" "alice_all" {
  registry_id  = ccp_registry.main.id
  user_id      = ccp_registry_user.alice.id
  repo_pattern = "*"
  actions      = ["*"]
}

resource "ccp_registry_acl" "ci_push_myapp" {
  registry_id  = ccp_registry.main.id
  user_id      = ccp_registry_user.ci_pull.id
  repo_pattern = "myapp/*"
  actions      = ["pull", "push"]
}

# ─── Outputs ───────────────────────────────────────────────────────────────

output "registry_hostname" {
  description = "Use as the docker hostname: docker login <hostname>."
  value       = ccp_registry.main.hostname
}

output "registry_admin_username" {
  value = ccp_registry.main.admin_username
}

output "registry_admin_password" {
  description = "One-shot admin password — captured at creation, never re-emitted by the API."
  value       = ccp_registry.main.admin_password
  sensitive   = true
}

output "alice_password" {
  value     = ccp_registry_user.alice.password
  sensitive = true
}

output "ci_pull_password" {
  value     = ccp_registry_user.ci_pull.password
  sensitive = true
}
