---
page_title: "ccp_org_member Resource - ccp"
subcategory: "Identity"
description: |-
  Manages a member and their role within an organization on CETIC Cloud Platform.
---

# ccp_org_member (Resource)

Manages a member within a CETIC Cloud organization. Members are invited by email address and assigned a role that controls their permissions within the organization. If the invited email already has a CETIC Cloud account, the membership is linked automatically. Otherwise, the user must register with that email address to activate the membership.

~> **Note:** The `role` argument is mutable — you can change a member's role without recreating the resource. The `org_id` and `email` arguments are immutable; changing either forces a new invitation.

## Example Usage

```hcl
resource "ccp_org_member" "alice_admin" {
  org_id = ccp_organization.engineering.id
  email  = "alice@acme.example.com"
  role   = "admin"
}

resource "ccp_org_member" "bob_dev" {
  org_id = ccp_organization.engineering.id
  email  = "bob@acme.example.com"
  role   = "member"
}

resource "ccp_org_member" "auditor" {
  org_id = ccp_organization.engineering.id
  email  = "auditor@external-firm.example.com"
  role   = "viewer"
}
```

## Argument Reference

### Required

- `org_id` - (Required, Forces new resource) UUID of the organization to invite the member to.
- `email` - (Required, Forces new resource) Email address of the person to invite.
- `role` - (Required) Role to assign. One of: `viewer` (read-only access), `member` (create/manage resources), `admin` (all operations except billing and org deletion). The `owner` role cannot be assigned — it belongs to the account that created the organization. Mutable without forcing a new resource.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the membership record.
- `status` - Membership status. Possible values: `pending` (invited, not yet accepted), `active` (linked to an account).

## Import

Organization memberships can be imported using their UUID:

```
terraform import ccp_org_member.alice_admin <membership_id>
```
