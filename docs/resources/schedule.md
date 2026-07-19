---
page_title: "ccp_schedule Resource - ccp"
subcategory: "Scheduler"
description: |-
  Weekly start/stop planner — powers a VM, container, scale set, Kubernetes node pool or database instance off during declared windows and back on outside of them.
---

# ccp_schedule (Resource)

Manages a **start/stop schedule** on CETIC Cloud. A schedule powers its
target resource **off** during the weekly windows you declare and back
**on** the rest of the time — the classic way to cut the bill on
non-production workloads by turning them off nights and week-ends.

The target is addressed polymorphically by `resource_type` +
`resource_id`, so a single schedule type drives virtual machines,
containers, scale sets, Kubernetes node pools or database instances.

~> **Stopping is not destroying.** A scheduled-off resource is powered
down, never deleted. For a `db_instance` the stored data is kept — only
the compute is scaled to zero — so storage keeps billing while compute
charges pause. Deleting the schedule powers the target back on.

~> **`resource_type` and `resource_id` are immutable.** Re-pointing a
schedule at a different target forces destroy + create. `name`,
`timezone`, `enabled` and `windows` are all mutable in place.

~> **Windows are validated to prevent flapping.** Saving is billed by the
hour, so the platform rejects windows shorter than one hour, gaps shorter
than one hour, overlapping windows, or more than two on/off cycles per day
with a `422` and a clear message (e.g. *"a stop shorter than one hour or
more than two cycles a day brings no saving — space your windows at least
one hour apart"*). The provider surfaces that message verbatim in the plan
error.

## Example Usage

### Turn a VM off nights and week-ends

Two OFF windows: a long one across the week-end (Friday 20:00 → Monday
08:00) plus weeknight windows (20:00 → 08:00). Powered on during working
hours only.

```hcl
resource "ccp_schedule" "office_hours" {
  name          = "webapp-office-hours"
  resource_type = "vm"
  resource_id   = ccp_vm_instance.app.id
  timezone      = "Europe/Paris"

  windows = [
    # Week-end: OFF from Friday 20:00 to Monday 08:00
    { start_day = 4, start_hour = 20, end_day = 0, end_hour = 8 },
    # Weeknights: OFF 20:00 → 08:00 (Mon→Tue, Tue→Wed, Wed→Thu, Thu→Fri)
    { start_day = 0, start_hour = 20, end_day = 1, end_hour = 8 },
    { start_day = 1, start_hour = 20, end_day = 2, end_hour = 8 },
    { start_day = 2, start_hour = 20, end_day = 3, end_hour = 8 },
    { start_day = 3, start_hour = 20, end_day = 4, end_hour = 8 },
  ]
}
```

### Turn a Kubernetes node pool off over the week-end

`resource_type = "ccks_node_pool"` targets a single node pool — the
control plane and the other pools are untouched.

```hcl
resource "ccp_schedule" "ci_pool_weekend" {
  name          = "ci-pool-weekend-off"
  resource_type = "ccks_node_pool"
  resource_id   = ccp_k8s_node_pool.ci.id
  timezone      = "Europe/Paris"

  windows = [
    # Friday 20:00 → Monday 08:00
    { start_day = 4, start_hour = 20, end_day = 0, end_hour = 8 },
  ]
}
```

### Keep a plan without applying it

Set `enabled = false` to keep the schedule defined but paused — the target
stays in whatever power state it is currently in.

```hcl
resource "ccp_schedule" "staging_db" {
  name          = "staging-db-nights"
  resource_type = "db_instance"
  resource_id   = ccp_db_pg_instance.staging.id
  enabled       = false

  windows = [
    { start_day = 0, start_hour = 22, end_day = 1, end_hour = 7 },
  ]
}
```

## Argument Reference

### Required

- `name` - (Required) Human-readable label, unique within the org
  (max 63 chars). Mutable in place.
- `resource_type` - (Required, ForceNew) Kind of resource the schedule
  drives. One of `vm`, `container`, `vm_scale_set`,
  `container_scale_set`, `ccks_node_pool` (a single node pool, not the
  whole cluster) or `db_instance`. Changing forces replacement.
- `resource_id` - (Required, ForceNew) UUID of the target resource. For
  `ccks_node_pool` this is the node pool id (`ccp_k8s_node_pool.id`), not
  the cluster id. Changing forces replacement.
- `windows` - (Required) A list of one or more weekly OFF interval objects
  (at least one). See [below](#windows).

### Optional

- `timezone` - (Optional) IANA timezone the windows are interpreted in
  (e.g. `Europe/Paris`, `Africa/Abidjan`). Defaults to `Europe/Paris`.
  Mutable in place.
- `enabled` - (Optional) Whether the schedule actively drives the target.
  When `false` the plan is kept but never applied. Defaults to `true`.
  Mutable in place.

### `windows`

Each `windows` element is one weekly OFF interval. The target is powered off
during `[start → end)` (end exclusive) and on outside of it. When `end` is
earlier in the week than `start`, the interval wraps across the week-end.

- `start_day` - (Required) Day the OFF interval starts: `0`=Monday …
  `6`=Sunday.
- `start_hour` - (Required) Hour the OFF interval starts, whole hour
  (`0..24`, `HH:00`).
- `end_day` - (Required) Day the OFF interval ends: `0`=Monday …
  `6`=Sunday.
- `end_hour` - (Required) Hour the OFF interval ends, whole hour
  (`0..24`, `HH:00`).

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - Server-assigned UUID of the schedule.
- `current_state` - Last desired power state applied by the platform:
  `on` or `off`.
- `last_transition_at` - RFC 3339 timestamp of the last power transition,
  or null if none yet.
- `estimated_monthly_fee_cents` - Estimated monthly scheduler fee in cents
  (number of driven instances × the per-instance rate).

## Import

Schedules are imported using their UUID:

```
terraform import ccp_schedule.office_hours <schedule_id>
```
