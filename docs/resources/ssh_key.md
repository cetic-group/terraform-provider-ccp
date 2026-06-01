---
page_title: "ccp_ssh_key Resource - ccp"
subcategory: "Identity"
description: |-
  Manages an SSH public key on CETIC Cloud Platform.
---

# ccp_ssh_key (Resource)

Manages an SSH public key registered on CETIC Cloud. Keys registered here can be injected into container instances and VM instances at creation time by referencing their UUID in the `ssh_key_ids` argument. Keys are stored as their public key only — the private key never leaves your machine.

The visibility of a key is controlled by `scope` — three values (`user`, `org`, `tenant`) trade off reach for the permission level required to create the key:

| Scope     | Visibility                                                                 | Required role for create     |
|-----------|----------------------------------------------------------------------------|------------------------------|
| `user`    | Only the creator. Survives org switches — useful for personal CI tokens.   | Any member (default).        |
| `org`     | Every member of the **currently active** organization.                     | Org `admin` or `owner`.      |
| `tenant`  | Every org **and** every invited member of the tenant.                      | Tenant `owner` only.         |

~> **Note:** `name`, `public_key` and `scope` are all immutable after creation. To rotate a key or change its visibility, create a new `ccp_ssh_key` resource, update references, then delete the old one. The CETIC Cloud API has no PATCH endpoint for SSH keys.

## Example Usage

```hcl
# Load from a local file — defaults to scope = "user" (personal).
resource "ccp_ssh_key" "ops_team" {
  name       = "ops-team-ed25519"
  public_key = file("~/.ssh/id_ed25519.pub")
}

# Inline key shared across an organization — admin+/owner required.
resource "ccp_ssh_key" "ci_deploy" {
  name       = "ci-deploy"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBQqn0OHfSEwi1FfbxLFz9e3M+B4N/kd0aeGFRfxKlXa ci@acme.example.com"
  scope      = "org"
}

# Tenant-wide platform key — owner-only, visible across every org.
resource "ccp_ssh_key" "platform_master" {
  name       = "platform-master"
  public_key = file("~/.ssh/platform_master.pub")
  scope      = "tenant"
}

# Use in container instances
resource "ccp_container_instance" "web" {
  name        = "web-01"
  region      = "RNN"
  plan        = "small"
  template    = "ubuntu-24.04"
  vnet_id     = ccp_vnet.web.id
  ssh_key_ids = [ccp_ssh_key.ops_team.id, ccp_ssh_key.ci_deploy.id]
}
```

## Argument Reference

### Required

- `name` - (Required, Forces new resource) Human-readable label for the SSH key. Shown in the console and CLI.
- `public_key` - (Required, Forces new resource) OpenSSH public key string. Supported key types: `ssh-ed25519`, `ssh-rsa`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp521`.

### Optional

- `scope` - (Optional, Forces new resource) Visibility scope. One of `user` (default), `org`, `tenant`. See the table at the top of this page for the permission required to create each scope. Defaults to `user`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the SSH key.
- `fingerprint` - SHA-256 fingerprint of the public key in the format `SHA256:...`.
- `created_by_tenant_id` - UUID of the tenant the key was created from. Read-only. Null on legacy keys predating the scoping migration.
- `created_at` - RFC 3339 timestamp at which the key was registered.

## Import

SSH keys can be imported using their UUID:

```
terraform import ccp_ssh_key.ops_team <ssh_key_id>
```

After import, the `public_key` attribute will appear as drift on the next plan (the API never returns the raw key body) — re-apply once to reconcile state, or set `lifecycle { ignore_changes = [public_key] }` if you do not own the original material.
