---
page_title: "ccp_registry_cleanup_schedule Resource - ccp"
subcategory: "Registry"
description: |-
  Scheduled cleanup of the images of a CETIC Container Registry.
---

# ccp_registry_cleanup_schedule (Resource)

Removes images of a `ccp_registry` on a schedule: tags selected by
repository patterns × tag patterns and a criterion, run on an optional cron
(UTC). Each registry accepts up to **4** schedules.

A run never deletes an image on the spot: the selected images go to the
registry's **recycle bin** and are only purged after the registry's grace
period (7 days by default), restorable until then. Images protected by the
registry's `keep` retention rules are never selected.

~> **A scheduled run condemns images without a prior simulation.** Simulate a
schedule before giving it a `cron`. Simulations and immediate runs are
actions, not state: they are available in the console and the API, and are
deliberately not managed by Terraform — an `apply` never triggers a run.

## Example Usage

```hcl
resource "ccp_registry" "main" {
  name           = "ccr-prod"
  region         = "RNN"
  expose_public  = false
  expose_private = true
}

# Every night at 03:00 UTC, keep the 5 most recent pre-release tags
# of each app/* repository.
resource "ccp_registry_cleanup_schedule" "prereleases" {
  registry_id   = ccp_registry.main.id
  name          = "prereleases"
  repo_patterns = ["app/*"]
  tag_patterns  = ["*-rc-*", "*-dev"]
  criterion     = "keep_last_n"
  value         = 5
  cron          = "0 3 * * *"
}

# On demand only (no cron): every feature image older than 30 days.
resource "ccp_registry_cleanup_schedule" "features" {
  registry_id   = ccp_registry.main.id
  name          = "old features"
  repo_patterns = ["app/*", "tools/*"]
  tag_patterns  = ["feature-*"]
  criterion     = "older_than_days"
  value         = 30
}
```

## Argument Reference

- `registry_id` - (Required, Forces new resource) UUID of the parent
  `ccp_registry`. The registry must be `active` when the schedule is created.
- `name` - (Required) Unique within the registry: 1 to 64 characters,
  starting with a letter or a digit, then letters, digits, spaces and
  `. _ -`. Leading and trailing spaces are removed by the platform.
- `repo_patterns` - (Required) 1 to 10 distinct repository patterns:
  lowercase letters, digits, `. _ - /` and the wildcards `*` (any sequence of
  characters, `/` included) and `?` (one character); no `**`; 255 characters
  at most.
- `tag_patterns` - (Required) 1 to 10 distinct tag patterns: letters,
  digits, `. _ -` and the wildcards `*` `?`; no `**`; 128 characters at most.
- `criterion` - (Required) One of:
  - `all` — every tag matching the patterns;
  - `older_than_days` — tags older than `value` days;
  - `keep_last_n` — keep the `value` most recent tags per repository and per
    pair of patterns, select the others.
- `value` - (Optional) Days (1 to 3650) for `older_than_days`, number of tags
  kept (1 to 1000) for `keep_last_n`. Must be omitted for `all`.
- `cron` - (Optional) 5-field cron expression in UTC, evaluated every 5
  minutes: `*`, `*/N`, values, ranges `a-b` and lists `a,b` (no `*` inside a
  list); day of month and day of week cannot both be restricted. Omitted (or
  empty), the schedule only runs on demand: removing `cron` from the
  configuration makes an existing schedule manual.
- `enabled` - (Optional) `false`: the schedule never runs, scheduled or on
  demand (it can still be simulated). Defaults to `true`.

Patterns that match every repository **and** every tag (`*`, `?*`, `*?`…; for
repositories any pattern made only of wildcards and `/`, such as `*/*`) are
refused with `all` — that would empty the whole registry — and require
`value` of at least 7 with `older_than_days`.

These rules are checked at plan time exactly as the platform checks them.
The limit of 4 schedules per registry and the uniqueness of `name` are
checked by the platform at apply time.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - UUID of the schedule.
- `slot` - Slot of the schedule in its registry (1 to 4), assigned at
  creation.
- `last_dispatched_at` - RFC 3339 time of the last scheduled dispatch, or of
  the last creation or update: the scheduler never catches up on a minute
  before it, so a run due just before an update is skipped.
- `created_at` - RFC 3339 creation timestamp.
- `updated_at` - RFC 3339 timestamp of the most recent update.

Deleting a schedule keeps its run history and the images already in the
recycle bin, shown under its name and still restorable until purged.

## Import

Schedules can be imported using `<registry_id>/<schedule_id>`:

```
terraform import ccp_registry_cleanup_schedule.prereleases <registry_id>/<schedule_id>
```
