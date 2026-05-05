---
page_title: "ccp_support_ticket Resource - cetic-cloud-platform"
subcategory: "Account"
description: |-
  Manages a support ticket on CETIC Cloud Platform.
---

# ccp_support_ticket (Resource)

Creates and manages a support ticket with the CETIC Cloud team. Use this resource to report incidents, request assistance, or ask technical questions. Tickets are tracked through to resolution and support chat is available from the console.

~> **Note:** `subject` and `message` are immutable after creation — changing either forces a new ticket. Only `priority` can be updated in place. Destroying this resource closes the ticket.

## Example Usage

```hcl
resource "ccp_support_ticket" "container_unreachable" {
  subject  = "Container unreachable after VNet migration"
  message  = <<-EOT
    Container ID: ${ccp_container_instance.web.id}
    Region: RNN
    Issue: SSH connection times out since moving the container to VNet ${ccp_vnet.web.id}.
    Last successful SSH: 2026-05-04T08:00:00Z.
    Steps taken: verified security group rules, confirmed IPAM lease is active.
  EOT
  priority = "high"
}
```

## Argument Reference

### Required

- `subject` - (Required, Forces new resource) Short summary of the issue (max 200 characters). Used as the ticket title in the console.
- `message` - (Required, Forces new resource) Detailed description of the issue or request. Include resource IDs, region, steps to reproduce, and any error messages.

### Optional

- `priority` - (Optional) Ticket priority. One of: `low`, `normal`, `high`, `urgent`. Defaults to `normal`. Mutable without forcing a new resource. Use `urgent` only for production-down incidents.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the support ticket.
- `status` - Current status. Possible values: `open`, `in_progress`, `waiting`, `resolved`, `closed`.
- `ticket_number` - Human-readable ticket reference (e.g. `TKT-00247`). Use this when communicating with the support team.
- `created_at` - Timestamp when the ticket was created (RFC3339).

## Import

Support tickets can be imported using their UUID:

```
terraform import ccp_support_ticket.container_unreachable <ticket_id>
```
