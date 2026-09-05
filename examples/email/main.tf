# Hosted email — a mail domain, its mailboxes and its aliases.
#
# A domain is created ON HOLD: the name is reserved and nothing is routed until
# the ownership record is published in the public DNS of the domain. That is why
# this example is applied TWICE:
#
#   1. `terraform apply` with `verify_domain = false` — returns the records to
#      publish (see the `records_to_publish` output);
#   2. publish them at your DNS host, then
#      `terraform apply -var verify_domain=true` — activates the domain and
#      creates the mailboxes.
#
# A mailbox on a domain that is not active is refused: it would have nowhere to
# exist. Hence the `count` on the mailboxes below.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 6.5"
    }
  }
}

provider "ccp" {
  # api_key is read from CCP_API_KEY when omitted.
}

variable "domain" {
  type    = string
  default = "example.com"
}

variable "verify_domain" {
  description = "Set to true on the second apply, once the ownership record is live."
  type        = bool
  default     = false
}

variable "contact_password" {
  description = "Mailbox password — 12 characters minimum."
  type        = string
  sensitive   = true
}

resource "ccp_email_domain" "this" {
  name                  = var.domain
  wait_for_verification = var.verify_domain
}

resource "ccp_email_account" "contact" {
  count = var.verify_domain ? 1 : 0

  address        = "contact@${var.domain}"
  password       = var.contact_password
  quota_gb       = 5
  displayed_name = "Sales"

  depends_on = [ccp_email_domain.this]
}

resource "ccp_email_alias" "team" {
  count = var.verify_domain ? 1 : 0

  address      = "team@${var.domain}"
  destinations = ["contact@${var.domain}"]
  comment      = "Shared team address"

  depends_on = [ccp_email_domain.this]
}

# `verification` is kept apart from the rest because it is the only record that
# BLOCKS activation. The others (MX, SPF, DKIM, DMARC) decide whether mail is
# delivered and trusted, and each one reports the state observed in your zone.
output "records_to_publish" {
  description = "Publish these in the public DNS of the domain; `status` says what is left to do."
  value = concat(
    [ccp_email_domain.this.verification],
    ccp_email_domain.this.dns_records,
  )
}

output "mail_client_settings" {
  description = "Servers to enter in a mail application. The user name is the full address."
  value       = ccp_email_domain.this.client_config
}
