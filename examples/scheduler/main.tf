# End-to-end example for the start/stop Scheduler — turns non-production
# workloads off nights and week-ends to cut the compute bill:
#   - a VM powered off every weeknight and all week-end,
#   - a Kubernetes CI node pool powered off over the week-end only.
#
# Stopping never destroys: the resources are powered down and back up on
# schedule, storage is preserved throughout.

terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/ccp"
      version = "~> 5.0"
    }
  }
}

provider "ccp" {} # reads CCP_API_KEY

# ─── Inputs ──────────────────────────────────────────────────────────────────

variable "vm_id" {
  type        = string
  description = "UUID of the VM instance to schedule (ccp_vm_instance.id)."
}

variable "node_pool_id" {
  type        = string
  description = "UUID of the CCKS node pool to schedule (ccp_k8s_node_pool.id — the pool, not the cluster)."
}

variable "timezone" {
  type        = string
  default     = "Europe/Paris"
  description = "IANA timezone the windows are interpreted in."
}

# ─── VM — off nights and week-ends ────────────────────────────────────────────
# One long week-end window (Fri 20:00 → Mon 08:00) plus a window per
# weeknight (20:00 → 08:00). On during working hours only.

resource "ccp_schedule" "vm_office_hours" {
  name          = "vm-office-hours"
  resource_type = "vm"
  resource_id   = var.vm_id
  timezone      = var.timezone

  windows = [
    { start_day = 4, start_hour = 20, end_day = 0, end_hour = 8 }, # Fri 20:00 → Mon 08:00
    { start_day = 0, start_hour = 20, end_day = 1, end_hour = 8 }, # Mon → Tue
    { start_day = 1, start_hour = 20, end_day = 2, end_hour = 8 }, # Tue → Wed
    { start_day = 2, start_hour = 20, end_day = 3, end_hour = 8 }, # Wed → Thu
    { start_day = 3, start_hour = 20, end_day = 4, end_hour = 8 }, # Thu → Fri
  ]
}

# ─── CCKS node pool — off over the week-end ──────────────────────────────────
# The control plane and other pools keep running.

resource "ccp_schedule" "ci_pool_weekend" {
  name          = "ci-pool-weekend-off"
  resource_type = "ccks_node_pool"
  resource_id   = var.node_pool_id
  timezone      = var.timezone

  windows = [
    { start_day = 4, start_hour = 20, end_day = 0, end_hour = 8 }, # Fri 20:00 → Mon 08:00
  ]
}

# ─── Outputs ─────────────────────────────────────────────────────────────────

output "vm_schedule_state" {
  description = "Current desired power state of the VM (on|off)."
  value       = ccp_schedule.vm_office_hours.current_state
}

output "estimated_monthly_fee_cents" {
  description = "Combined estimated monthly scheduler fee, in cents."
  value       = ccp_schedule.vm_office_hours.estimated_monthly_fee_cents + ccp_schedule.ci_pool_weekend.estimated_monthly_fee_cents
}
