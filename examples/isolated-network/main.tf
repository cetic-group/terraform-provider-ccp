# An isolated subnet, and a Kubernetes cluster inside it.
#
# `snat = false` is the one setting that makes a subnet isolated: no outbound
# internet access, and NO PUBLIC ADDRESS can be attached to anything in it. A
# cluster runs there all the same — its node images are preloaded and name
# resolution is served from inside the network — so it is reached over a private
# path instead.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 6.3"
    }
  }
}

provider "ccp" {
  # api_key is read from CCP_API_KEY when omitted.
}

variable "region" {
  type    = string
  default = "RNN"
}

resource "ccp_vpc" "main" {
  name   = "airgap"
  region = var.region
}

resource "ccp_vnet" "airgap" {
  vpc_id = ccp_vpc.main.id
  name   = "airgap-workers"
  cidr   = "10.30.0.0/24"
  snat   = false # isolated
}

resource "ccp_k8s_cluster" "airgap" {
  name            = "airgap-cluster"
  region          = var.region
  vpc_id          = ccp_vpc.main.id
  vnet_id         = ccp_vnet.airgap.id
  k8s_version     = "v1.34.8"
  os_template_key = "kube-v1-34-8"

  # `external` would need a public address, which this subnet cannot have.
  ingress_controller_enabled = true
  ingress_controller_scope   = "internal"

  initial_pool {
    name     = "workers"
    plan     = "medium"
    replicas = 2
  }
}

# The way in: a private path, since no public address can be attached here.
resource "ccp_bastion" "airgap" {
  name   = "airgap-bastion"
  region = var.region
  vpc_id = ccp_vpc.main.id
}

output "isolated" {
  description = "The subnet has no outbound internet access."
  value       = !ccp_vnet.airgap.snat
}

output "apiserver_endpoint" {
  description = "Private endpoint of the apiserver — reachable from inside the network."
  value       = ccp_k8s_cluster.airgap.apiserver_internal_ip
}
