---
page_title: "ccp_organization Resource - ccp"
subcategory: "Identity"
description: |-
  Manages an organization on CETIC Cloud Platform.
---

# ccp_organization (Resource)

Manages an organization on CETIC Cloud Platform. Organizations group resources and team members under a shared billing account. Members can be invited with granular roles (`admin`, `member`, `viewer`) using `ccp_org_member`. Resources created within an organization share quotas and billing.

## Example Usage

```hcl
resource "ccp_organization" "engineering" {
  name        = "Acme Engineering"
  description = "Main engineering team — all production infrastructure"
}

resource "ccp_org_member" "alice" {
  org_id = ccp_organization.engineering.id
  email  = "alice@acme.example.com"
  role   = "admin"
}

resource "ccp_org_member" "bob" {
  org_id = ccp_organization.engineering.id
  email  = "bob@acme.example.com"
  role   = "member"
}

output "org_id" {
  value = ccp_organization.engineering.id
}
```

## Argument Reference

### Required

- `name` - (Required) Display name of the organization.

### Optional

- `description` - (Optional) Description of the organization and its purpose.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the organization.

## Import

Organizations can be imported using their UUID:

```
terraform import ccp_organization.engineering <org_id>
```
