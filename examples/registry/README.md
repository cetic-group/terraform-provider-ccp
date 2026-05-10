# `examples/registry` — CETIC Container Registry end-to-end

Provisions a complete CCR (CETIC Container Registry) stack:

- A `ccp_vpc` and a `ccp_vnet` for the registry to run in.
- A `ccp_public_ip` attached to the registry (`exposure = "public"`).
- A `ccp_registry` with weekly GC scheduled Sunday 03:00 UTC.
- Two `ccp_registry_user` accounts:
  - `alice` — human admin, full access via the `*:*` ACL.
  - `ci-pull` — pipeline account scoped to `myapp/*` with `pull`+`push`.
- Two `ccp_registry_acl` rules wiring users to repository patterns.

## Usage

```bash
export CCP_API_KEY="ccp_live_..."

terraform init
terraform apply
```

Provisioning takes ~5-10 min while Let's Encrypt issues the certificate
and the LXC stack converges. Once the registry reports `status = "active"`:

```bash
HOST=$(terraform output -raw registry_hostname)
PASS=$(terraform output -raw registry_admin_password)

docker login $HOST --username admin --password "$PASS"
docker tag busybox:latest $HOST/myapp/sample:latest
docker push $HOST/myapp/sample:latest
```

The `ci-pull` user can do the same against the `myapp/*` namespace:

```bash
CI_PASS=$(terraform output -raw ci_pull_password)
docker login $HOST --username ci-pull --password "$CI_PASS"
docker pull $HOST/myapp/sample:latest
```

## Switching to a private registry

For workloads inside CCKS or peer VNets only, change `exposure` to `private`
and remove `public_ip_id`:

```hcl
resource "ccp_registry" "main" {
  name     = "ccr-demo"
  region   = "RNN"
  vpc_id   = ccp_vpc.main.id
  vnet_id  = ccp_vnet.registry.id
  exposure = "private"   # DNS-01 IONOS, no public IP
}
```

In CCKS, the cluster-agent injects pull credentials transparently — no
`imagePullSecret` configuration needed for in-cluster workloads.

## Cleanup

```bash
terraform destroy
```
