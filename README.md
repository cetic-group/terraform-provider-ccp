# CETIC Cloud Platform Terraform Provider

Terraform provider for CETIC Cloud — sovereign cloud by CETIC Group.

> **Status — v0.3 (preview)**
>
> **8 resources + 2 data sources** are implemented: `ccp_ssh_key`,
> `ccp_vpc`, `ccp_vnet`, `ccp_container_instance`,
> `ccp_block_volume`, `ccp_public_ip`, `ccp_object_bucket`,
> `ccp_vm_instance`, plus the `ccp_regions` and
> `ccp_organizations` data sources. The full roadmap (load balancers,
> databases, K8s, blockchain…) lives in [`NOTES.md`](./NOTES.md).

---

## Quick start

### 1. Get an API key

In the CETIC Cloud console, go to **Security → API keys** and create one with
`write` scope. Tokens look like `ccp_live_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`
and are shown **only once** at creation.

### 2. Configure the environment

```bash
export CCP_API_KEY="ccp_live_..."
export CCP_API_URL="https://api.in.techledger.io"   # optional, this is the default
```

### 3. Declare the provider

```hcl
terraform {
  required_providers {
    ccp = {
      source  = "cetic-group/cetic-cloud-platform"
      version = "~> 0.3"
    }
  }
}

provider "ccp" {}
```

A full working example (SSH key, VPC, two VNets, region listing) lives in
[`examples/basic/main.tf`](./examples/basic/main.tf).

---

## Resources

| Name                          | Status | Notes                                                       |
|-------------------------------|--------|-------------------------------------------------------------|
| `ccp_ssh_key`           | ready  | No update — all changes force replacement.                  |
| `ccp_vpc`               | ready  | Async create/delete, polls until `active`.                  |
| `ccp_vnet`              | ready  | Nested under VPC. PATCH supports `name`, `snat`.            |
| `ccp_container_instance`| ready  | LXC. Async, polls until `running` + IP. All fields replace. |
| `ccp_block_volume`      | ready  | Ceph RBD. `size_gb` can grow, attach/detach via `attached_to_*`. |
| `ccp_public_ip`         | ready  | Allocate by region, attach to container/VM via `attached_to_*`. |
| `ccp_object_bucket`     | ready  | Ceph RGW S3. `is_public` mutable, master S3 creds in state (sensitive). |
| `ccp_vm_instance`       | ready  | QEMU VM. Async, polls until `running`. PATCH supports `name` + `tags`. |

## Data sources

| Name                       | Status | Notes                                                          |
|----------------------------|--------|----------------------------------------------------------------|
| `ccp_regions`        | ready  | Lists active regions (RNN/PAR/ABJ).                            |
| `ccp_organizations`  | ready  | Lists orgs accessible to the current API key's tenant.         |

## Multi-organization

Each CETIC Cloud API key is **bound to a single organization**
(`api_keys.org_id` in the API). The provider does **not** offer an
`organization` argument — to target a different org, use a different API
key via Terraform's provider aliases:

```hcl
provider "ccp" {
  # default — reads CCP_API_KEY (org "prod")
}

provider "ccp" {
  alias    = "staging"
  api_key  = var.ccp_staging_key   # org "staging"
}

resource "ccp_vpc" "prod" {
  name   = "prod"
  region = "RNN"
}

resource "ccp_vpc" "staging" {
  provider = ccp.staging
  name     = "staging"
  region   = "RNN"
}
```

To discover which orgs your account can access (useful for picking the
right API key), use the data source:

```hcl
data "ccp_organizations" "all" {}

output "accessible_orgs" {
  value = [for o in data.ccp_organizations.all.organizations : {
    id         = o.id
    name       = o.name
    is_default = o.is_default
  }]
}
```

---

## Provider configuration

| Argument   | Env var       | Default                       | Description                    |
|------------|---------------|-------------------------------|--------------------------------|
| `api_key`  | `CCP_API_KEY`  | _(required)_                          | API key, format `ccp_live_*`.        |
| `endpoint` | `CCP_API_URL` | `https://api.in.techledger.io`   | Base URL of the CETIC Cloud API.    |

```hcl
provider "ccp" {
  # api_key  = "ccp_live_..."             # prefer the env var
  # endpoint = "https://api.in.techledger.io"
}
```

---

## Local development

The provider is built with Go 1.22+ and the Terraform Plugin Framework.

```bash
# Build the binary in the current directory
make build

# Build + install into the local Terraform plugin cache so a sibling
# terraform init can pick it up:
#   ~/.terraform.d/plugins/registry.terraform.io/cetic-group/cetic-cloud-platform/0.3.0/<os>_<arch>/
make install

# Other helpers
make fmt    # gofmt -w .
make vet    # go vet ./...
make tidy   # go mod tidy
make clean  # remove the local binary + the installed plugin
```

After `make install`, in any example directory:

```bash
cd examples/basic
terraform init
terraform plan
```

Terraform will discover the locally installed build and skip the registry.

---

## License

MIT.
