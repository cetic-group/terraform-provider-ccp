---
page_title: "ccp_ssh_key Resource - cetic-cloud-platform"
subcategory: "Identity"
description: |-
  Manages an SSH public key on CETIC Cloud Platform.
---

# ccp_ssh_key (Resource)

Manages an SSH public key registered on CETIC Cloud. Keys registered here can be injected into container instances and VM instances at creation time by referencing their UUID in the `ssh_key_ids` argument. Keys are stored as their public key only — the private key never leaves your machine.

~> **Note:** `public_key` is immutable after creation. To rotate a key, create a new `ccp_ssh_key` resource, update references, and delete the old one.

## Example Usage

```hcl
# Load from a local file
resource "ccp_ssh_key" "ops_team" {
  name       = "ops-team-ed25519"
  public_key = file("~/.ssh/id_ed25519.pub")
}

# Inline key for a CI/CD service account
resource "ccp_ssh_key" "ci_deploy" {
  name       = "ci-deploy"
  public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBQqn0OHfSEwi1FfbxLFz9e3M+B4N/kd0aeGFRfxKlXa ci@acme.example.com"
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

- `name` - (Required) Human-readable label for the SSH key. Shown in the console and CLI.
- `public_key` - (Required, Forces new resource) OpenSSH public key string. Supported key types: `ssh-ed25519`, `ssh-rsa`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp521`.

## Attributes Reference

In addition to all arguments above, the following attributes are exported:

- `id` - The UUID of the SSH key.
- `fingerprint` - SHA-256 fingerprint of the public key in the format `SHA256:...`.

## Import

SSH keys can be imported using their UUID:

```
terraform import ccp_ssh_key.ops_team <ssh_key_id>
```
