# `examples/scheduler` — start/stop planner end-to-end

Turns non-production workloads off outside working hours to cut the compute
bill, using the `ccp_schedule` resource:

- A **VM** powered off every weeknight (20:00 → 08:00) and all week-end
  (Friday 20:00 → Monday 08:00).
- A **CCKS node pool** powered off over the week-end only — the control
  plane and the cluster's other pools keep running.

Stopping never destroys the targets: they are powered down and back up on
schedule, and storage is preserved throughout. Deleting a schedule powers
its target back on.

## Usage

```bash
export CCP_API_KEY="ccp_live_..."

export TF_VAR_vm_id="11111111-2222-3333-4444-555555555555"        # ccp_vm_instance.id
export TF_VAR_node_pool_id="66666666-7777-8888-9999-000000000000" # ccp_k8s_node_pool.id
# Optional: override the timezone (defaults to Europe/Paris).
export TF_VAR_timezone="Europe/Paris"

terraform init
terraform apply
```

## Notes

- `resource_type` + `resource_id` address the target; both are immutable
  (changing either forces a replace).
- Windows are validated server-side to avoid flapping: each OFF and each ON
  interval must be at least one hour and you may not exceed two on/off
  cycles per 24 h. A rejected plan surfaces the platform's business message
  directly.
- `current_state` (`on`/`off`) and `estimated_monthly_fee_cents` are
  computed and refresh on read.
