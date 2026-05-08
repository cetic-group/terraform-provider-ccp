---
page_title: "ccp_db_mysql_credentials Data Source - cetic-cloud-platform"
subcategory: "Databases"
description: |-
  Fetches the admin credentials of a MySQL-compatible managed instance.
---

# ccp_db_mysql_credentials (Data Source)

Read-only datasource that fetches the admin credentials of a MySQL-compatible
managed instance from the CETIC Cloud API. Returns username + password + database + host:port + uri.

~> **Sensitive output.** The password is never written to plain log files
by Terraform, but the data lands in your **state file** unencrypted —
make sure your state backend is encrypted (S3 with SSE, GCS, or a remote
backend like Terraform Cloud).

## Example Usage

```hcl
resource "ccp_db_mysql_instance" "db" {
  name           = "app-db"
  region         = "RNN"
  vpc_id         = ccp_vpc.main.id
  vnet_id        = ccp_vnet.data.id
  plan           = "small"
  storage_gb     = 20
  replicas       = 1
}

data "ccp_db_mysql_credentials" "db" {
  id = ccp_db_mysql_instance.db.id
}

output "db_uri" {
  value     = data.ccp_db_mysql_credentials.db.uri
  sensitive = true
}
```

## Argument Reference

- `id` - (Required) UUID of the MySQL-compatible instance.

## Attributes Reference

- `username` - Admin username (empty for Valkey).
- `password` - Admin password. **Sensitive**.
- `database` - Admin database name (empty for Valkey).
- `host` - Endpoint host (private VNet IP).
- `port` - Endpoint port.
- `uri` - Connection URI ready to plug into a client. **Sensitive**.
