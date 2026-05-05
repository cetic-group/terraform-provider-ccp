---
page_title: "ccp_organizations Data Source - cetic-cloud-platform"
subcategory: "Identity"
description: |-
  Lists organizations accessible to the authenticated API key on CETIC Cloud Platform.
---

# ccp_organizations (Data Source)

Returns all organizations accessible to the authenticated user or API key, including organizations where the user holds any role (owner, admin, member, or viewer). Use this data source to dynamically reference organization IDs without hard-coding UUIDs.

## Example Usage

```hcl
data "ccp_organizations" "all" {}

# Output all accessible organizations as a map of name -> id
output "org_map" {
  value = {
    for org in data.ccp_organizations.all.organizations :
    org.name => org.id
  }
}

# Look up a specific organization by name
locals {
  engineering_org_id = one([
    for org in data.ccp_organizations.all.organizations :
    org.id if org.name == "Acme Engineering"
  ])
}

# Create a VPC inside a specific organization
resource "ccp_vpc" "main" {
  name   = "production"
  region = "RNN"
}
```

## Argument Reference

This data source has no arguments.

## Attributes Reference

The following attributes are exported:

- `organizations` - List of organization objects. Each object contains:
  - `id` - UUID of the organization.
  - `name` - Display name of the organization.
  - `role` - Your role in this organization. One of: `owner`, `admin`, `member`, `viewer`.
