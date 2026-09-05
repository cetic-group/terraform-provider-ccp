# End-to-end example for ccp_registry — VPC + VNet + registry
# + 2 users (admin/ci-pull) + 2 ACLs.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 6.6"
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

# ─── Registry ──────────────────────────────────────────────────────────────
#
# Exposure is two independent switches, not one mode: `expose_public` opens the
# registry to the internet, `expose_private` serves it inside the tenant's own
# networks — the VNet above and anything peered with it. Both can be on; both
# off is refused.
#
# The registry is not attached to a VNet: there is no `vpc_id` or `vnet_id` on
# this resource, and no public IP to reserve — the platform provisions and
# renews the certificate for `<slug>.cloud.cetic-group.com` itself.

resource "ccp_registry" "main" {
  name           = "ccr-demo"
  region         = "RNN"
  expose_public  = true
  expose_private = true
  storage_gb     = 50
  tags           = ["env:demo"]
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

output "registry_url" {
  description = "Use as the docker hostname: docker login <url>."
  value       = ccp_registry.main.url
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
