# Private DNS — a zone answered only inside your own private network.
#
# The machines of the network receive its name server automatically, AT THEIR
# CREATION. Declaring the zone before the machines is therefore the order that
# works; turning private DNS on in a populated network leaves the existing
# machines with the name server they already have.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 6.4"
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

resource "ccp_vpc" "corp" {
  name   = "corp"
  region = var.region
}

resource "ccp_vnet" "office" {
  vpc_id = ccp_vpc.corp.id
  name   = "office"
  cidr   = "10.20.0.0/24"
  snat   = true
}

resource "ccp_vnet" "workshop" {
  vpc_id = ccp_vpc.corp.id
  name   = "workshop"
  cidr   = "10.21.0.0/24"
  snat   = true
}

# The zone belongs to the PRIVATE NETWORK, not to one of its subnets: the name
# server places one address in each subnet and answers the same zones on all of
# them. Every zone of this network shares that server — and therefore its tier.
resource "ccp_dns_zone" "corp" {
  name        = "corp.internal"
  vpc_id      = ccp_vpc.corp.id
  tier        = "prod"
  default_ttl = 300

  # The subnets must exist first: the name server needs one leg in each.
  depends_on = [ccp_vnet.office, ccp_vnet.workshop]
}

resource "ccp_dns_record" "www" {
  zone_id = ccp_dns_zone.corp.id
  name    = "www"
  type    = "A"
  ttl     = 300
  records = ["10.20.0.10", "10.20.0.11"]
}

resource "ccp_dns_record" "apex_mx" {
  zone_id = ccp_dns_zone.corp.id
  name    = "@"
  type    = "MX"
  records = ["10 mail.corp.internal."]
}

resource "ccp_dns_record" "spf" {
  zone_id = ccp_dns_zone.corp.id
  name    = "@"
  type    = "TXT"
  # A TXT value carries its own quotes.
  records = ["\"v=spf1 -all\""]
}

data "ccp_dns_records" "corp" {
  zone_id = ccp_dns_zone.corp.id

  depends_on = [
    ccp_dns_record.www,
    ccp_dns_record.apex_mx,
    ccp_dns_record.spf,
  ]
}

# From a machine, use the address of ITS OWN subnet: they all answer the same
# zones, but each one is only reachable from its own subnet.
output "name_server_per_subnet" {
  description = "Address to configure as name server, per subnet."
  value = {
    for e in ccp_dns_zone.corp.resolver_endpoints : e.vnet_name => e.address
  }
}

output "records_maintained_by_the_platform" {
  description = "Read-only records that answer the apex of the zone."
  value = [
    for r in data.ccp_dns_records.corp.records : r.name
    if r.is_system_managed
  ]
}
