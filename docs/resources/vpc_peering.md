---
page_title: "ccp_vpc_peering Resource - ccp"
subcategory: "Networking"
description: |-
  Manages a peering connection between two VPCs on CETIC Cloud Platform.
---

# ccp_vpc_peering (Resource)

Peering inter-VPC : connecte deux VPCs entiers (tout le VPC voit le pair) au niveau L3, en IPs privées, sans traverser l'internet public. Une fois actif, toutes les instances des deux VPCs peuvent se joindre sur leurs adresses privées respectives.

Le peering est symétrique — ne déclarez qu'une seule ressource par couple de VPCs.

~> **Order at create-time is free:** the backend stores `vpc_a_id < vpc_b_id` (canonical order), but the provider canonicalizes the pair before sending it to the API, so you can write the UUIDs in any order in HCL. The state preserves whatever order you wrote. **Once stored, swapping `vpc_a_id` and `vpc_b_id` in HCL forces a replace** — Terraform sees it as an attribute change. Pick an order at create-time and stick with it.

## Example Usage

```hcl
resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "rnn"
}

resource "ccp_vpc" "staging" {
  name   = "staging"
  region = "rnn"
}

resource "ccp_vpc_peering" "prod_to_staging" {
  name    = "prod-to-staging"
  vpc_a_id = ccp_vpc.prod.id
  vpc_b_id = ccp_vpc.staging.id
  tags    = ["env:prod", "purpose:cross-vpc"]
}
```

## Argument Reference

### Required

- `name` - Human-readable name for the peering (2-100 chars). Forces replacement on change.
- `vpc_a_id` - UUID of one VPC. Order at create-time is free; once stored, any change (including swapping with `vpc_b_id`) forces replacement.
- `vpc_b_id` - UUID of the other VPC. Must be different from `vpc_a_id`. Forces replacement on change.

### Optional

- `tags` - List of free-form tags (max 60, max 50 chars each).

## Attributes Reference

- `id` - UUID of the VPC peering.
- `status` - One of `pending`, `active`, `deleting`, `error`.
- `tenant_id` - UUID of the tenant that owns this peering.
- `error_message` - Human-readable error detail when `status` is `error`, empty string otherwise.
- `created_at` - RFC3339 timestamp of creation.

## Import

```
terraform import ccp_vpc_peering.example <peering_id>
```
