---
page_title: "ccp_schedule Data Source - ccp"
subcategory: "Scheduler"
description: |-
  Look up a CETIC Cloud start/stop schedule by id or by name.
---

# ccp_schedule (Data Source)

Look up an existing start/stop schedule — either by `id` or by `name`.
Provide exactly one of the two.

## Example Usage

```hcl
data "ccp_schedule" "office_hours" {
  name = "webapp-office-hours"
}

output "next_fee_cents" {
  value = data.ccp_schedule.office_hours.estimated_monthly_fee_cents
}

output "currently" {
  value = data.ccp_schedule.office_hours.current_state # "on" | "off"
}
```

## Argument Reference

- `id` - (Optional) UUID of the schedule. Conflicts with `name`.
- `name` - (Optional) Schedule name, unique within the org. Conflicts with `id`.

## Attributes Reference

- `resource_type` - Kind of driven resource: `vm`, `container`,
  `vm_scale_set`, `container_scale_set`, `ccks_node_pool` or `db_instance`.
- `resource_id` - UUID of the driven resource.
- `timezone` - IANA timezone the windows are interpreted in.
- `enabled` - Whether the schedule actively drives the target.
- `windows` - List of weekly OFF intervals, each with `start_day`,
  `start_hour`, `end_day`, `end_hour`.
- `current_state` - Last desired power state applied: `on` or `off`.
- `last_transition_at` - RFC 3339 timestamp of the last power transition,
  or null.
- `estimated_monthly_fee_cents` - Estimated monthly scheduler fee in cents.
