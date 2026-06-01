---
page_title: "ccp_regions Data Source - ccp"
subcategory: "Networking"
description: |-
  Lists available regions on CETIC Cloud Platform.
---

# ccp_regions (Data Source)

Returns the list of regions available on CETIC Cloud Platform, including their availability status. Use this data source to dynamically select regions in your configurations rather than hard-coding region identifiers.

## Example Usage

```hcl
data "ccp_regions" "available" {}

# Deploy a VPC to every available region
resource "ccp_vpc" "per_region" {
  for_each = {
    for r in data.ccp_regions.available.regions :
    r.id => r if r.available
  }

  name   = "production-${lower(each.key)}"
  region = each.key
  tags   = ["env:prod", "multi-region"]
}

# Output only the IDs of available regions
output "available_region_ids" {
  value = [
    for r in data.ccp_regions.available.regions :
    r.id if r.available
  ]
}

# Output a map of region name -> id for use in other resources
output "region_map" {
  value = {
    for r in data.ccp_regions.available.regions :
    r.name => r.id if r.available
  }
}
```

## Argument Reference

This data source has no arguments.

## Attributes Reference

The following attributes are exported:

- `regions` - List of region objects. Each object contains:
  - `id` - Region identifier used in resource arguments (e.g. `RNN`, `PAR`, `ABJ`).
  - `name` - Human-readable region name (e.g. `Rennes`).
  - `location` - Geographic location description (e.g. `Rennes, France`).
  - `available` - Whether the region is currently accepting new resource creation.
