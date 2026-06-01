---
page_title: "ccp_quota_request Resource - ccp"
subcategory: "Account"
description: |-
  Submits a quota increase request on CETIC Cloud Platform.
---

# ccp_quota_request (Resource)

Submits a self-service quota increase request to the CETIC Cloud team. Requests are reviewed and approved or rejected by administrators. Only one pending request per quota field is allowed at a time — submitting a second request for the same field returns the existing pending request.

~> **Note:** This resource submits a request — it does not immediately change your quota. The quota limit is updated only after an administrator approves the request. Destroying a pending request cancels it. Destroying an approved or rejected request has no effect on the quota already applied.

## Example Usage

```hcl
# Request more container instances for Q3 scaling
resource "ccp_quota_request" "more_containers" {
  field           = "max_containers"
  requested_value = 50
  reason          = "Scaling our microservices deployment for Q3 product launch. Current limit of 10 is insufficient."
}

# Request more CPU cores for batch workloads
resource "ccp_quota_request" "more_cores" {
  field           = "max_cores"
  requested_value = 128
  reason          = "Running nightly batch ML training jobs that require 64 vCPUs peak concurrently."
}
```

## Argument Reference

### Required

- `field` - (Required, Forces new resource) The quota field to increase. Supported values: `max_containers`, `max_vms`, `max_scale_sets`, `max_vpcs`, `max_vnets`, `max_volumes`, `max_buckets`, `max_public_ips`, `max_load_balancers`, `max_cores`, `max_memory_mb`, `max_disk_gb`.
- `requested_value` - (Required, Forces new resource) The desired new limit value (must be higher than the current limit).
- `reason` - (Required, Forces new resource) Justification for the increase. Be specific — requests with detailed business context are approved faster.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the quota request.
- `status` - Current status. Possible values: `pending` (awaiting admin review), `approved` (quota increased), `rejected` (request declined).
- `granted_value` - The value actually granted by the administrator. May differ from `requested_value` if partially approved.
- `admin_note` - Administrator note explaining the decision (available after approval or rejection).
- `created_at` - Timestamp when the request was submitted (RFC3339).

## Import

Quota requests can be imported using their UUID:

```
terraform import ccp_quota_request.more_containers <request_id>
```
