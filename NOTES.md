# CETIC Cloud Terraform Provider — Notes & Roadmap

Internal notes for contributors. Audience: the next dev picking this up.

---

## What's implemented in v0.7.1

### Resources (30 total)

**Identity**
- `ccp_ssh_key` — Create / Read / Delete. No update.
- `ccp_api_key` — Create / Read / Delete. Sensitive `token` exposed once.
- `ccp_organization` — Create / Read / Update / Delete (multi-org).
- `ccp_org_member` — Invite + role management (admin / member / viewer).

**Network**
- `ccp_vpc` — Create / Read / Delete. Async, polls until `active`.
- `ccp_vnet` — Create / Read / Update (`name`, `snat`) / Delete. Async.
- `ccp_vnet_firewall_rule` — Per-VNet rules. Position-ordered.
- `ccp_vnet_ip_reservation` — Reserve a single IP or a range (`ip` + `range_end`).
- `ccp_vnet_peering` — Intra-VPC, between two VNets.
- `ccp_public_ip` — Allocate / attach via `attached_to_*` / detach / release.
- `ccp_ipaas_pool` — **admin only** — BYOIP routed pools.
- `ccp_load_balancer` — Highly available with floating VIP, automatic failover. Multi-listener, backends nested.

**Compute**
- `ccp_container_instance` — Linux containers. Create / Read / Delete. Polls until `running` + IP resolved.
- `ccp_container_scale_set` — Auto-scaled container group (`min_instances` / `max_instances` / `desired_instances`).
- `ccp_container_snapshot` — Point-in-time snapshot.
- `ccp_vm_instance` — Full virtual machines. Create / Read / Update (name, tags) / Delete.
- `ccp_vm_scale_set` — Auto-scaled VM group.
- `ccp_vm_snapshot` — Point-in-time VM snapshot.
- `ccp_k8s_cluster` — Managed Kubernetes (control plane + autoscaler config + ingress).
- `ccp_k8s_node_pool` — Worker pool with min/max + auto-scaling.
- `ccp_custom_template` — Snapshot a running container/VM into a reusable org-scoped template.

**Storage**
- `ccp_block_volume` — Resizable block storage. `size_gb` can grow; attach/detach via `attached_to_*`.
- `ccp_object_bucket` — S3-compatible bucket. Mutable `is_public`. Master S3 creds in state (sensitive).
- `ccp_object_storage_key` — Scoped subuser keys (`read` / `write` / `readwrite` / `full`).

**Database (managed)**
- `ccp_db_pg_instance` — PostgreSQL.
- `ccp_db_mysql_instance` — MySQL-compatible (MariaDB Galera under the hood).
- `ccp_db_valkey_instance` — Redis-compatible (Valkey).
- `ccp_db_ferretdb_instance` — MongoDB-compatible (FerretDB v2 + DocumentDB extension).

All four DB engines share the same shape: `region`, `vpc_id`, `vnet_id`, `plan`, `storage_gb`, `replicas` (1 = dev, 3 = prod HA), `engine_version`. The `tier` attribute is **Computed** — derived from `replicas` by the API.

**Support / Self-service**
- `ccp_support_ticket` — Create / Read / Update / Delete tickets.
- `ccp_quota_request` — Self-service quota increase requests.

### Data sources (7 total)

| Name | Use |
|---|---|
| `ccp_regions` | Active regions (`RNN`, `PAR`, `ABJ`). |
| `ccp_organizations` | Orgs accessible to the current API key's tenant. |
| `ccp_lxc_templates` | Container template catalog. Resolve `key` for `ccp_container_instance.template`. |
| `ccp_qemu_templates` | VM template catalog. Resolve `key` for `ccp_vm_instance.template`. |
| `ccp_k8s_templates` | Kubernetes node OS template catalog. |
| `ccp_db_plans` | Database plans, filterable by `engine`. |
| `ccp_db_engine_versions` | Active database engine versions, filterable by `engine`. |

---

## Architecture

**Plugin Framework, not SDK v2.** The provider is built on
`terraform-plugin-framework` (Hashicorp's current generation). Don't pull in
`terraform-plugin-sdk/v2` even for "simpler" resources — keeping the code on
one framework matters for diagnostics and schema consistency.

**Hand-written HTTP client at `internal/client/`.** It's a thin wrapper around
`net/http` with typed request/response structs per endpoint. No OpenAPI codegen.
Reasons: the CETIC Cloud API spec changes frequently while features land, and a
hand-rolled client lets us absorb the inconsistencies (see "API caveats" below)
in one place rather than fighting a generator.

**Auth — `ccp_live_*` API keys via Bearer header.** The CETIC Cloud API also
accepts JWTs for human users, but the provider only supports machine API keys.

**Server-side scoping via the auth context (`org_id`).** The API derives the
target organisation from the API key — there is no `tenant_id` / `org_id` in
request bodies. **Each API key is bound to exactly one organization**
(`api_keys.org_id` column in the backend). This also means: the provider
does **not** offer an `organisation` argument. To target a different org,
use a different API key — typically via Terraform provider aliases (see
the README's "Multi-organization" section for the canonical pattern).

The `ccp_organizations` data source lists orgs accessible to the
current key's tenant — useful for discovering which orgs are reachable, but
not for switching context within a single Terraform run. The `is_default`
flag marks the tenant's primary org. Membership orgs (where the tenant is
invited but not owner) appear after the owned ones.

---

## API caveats discovered

Things that surprised us while wiring this up. All confirmed against the
production API on `api.cloud.cetic-group.com`.

- **SSH keys — no GET single endpoint.** `GET /v1/ssh-keys/{id}` doesn't exist.
  The client does `GET /v1/ssh-keys` and filters client-side by ID. Fine for
  small fleets, will need pagination later.
- **SSH keys — no Update endpoint at all.** `PATCH` / `PUT` are not implemented
  server-side. Every attribute is therefore `RequiresReplace`.
- **VPC — `sdn_type` returned as a string, not a validated enum.** Free-form JSON.
  We type it as `types.String` and don't validate.
- **VNet — also no GET single endpoint.** Same trick: list under the parent VPC
  and filter by ID.
- **VNet — PATCH is partial.** The API only accepts `name` and `snat` in
  PATCH. Everything else (CIDR, VPC, region…) is `RequiresReplace`.
- **VNet — `isolated` is read-only via this resource.** The isolation toggle
  is managed via a dedicated firewall API; we expose it Computed only. To
  flip it, use the console or CLI for now.
- **VPC + VNet — async provisioning.** POST returns 201 immediately with
  `status: provisioning`. The provider polls until `active` (10s interval,
  3 min timeout).
- **Public IP — async attach.** Synchronous allocate, but `attached_to_*`
  triggers an IPaaS routing reconcile that takes 10–30 s (BGP convergence).
  Provider polls until `attached`.
- **Object bucket — master credentials only after `active`.** A second `GET`
  on `/credentials` is needed to populate `access_key` / `secret_key`.
- **Database `tier` attribute.** Computed — derived by the API from `replicas`
  (1 → `dev`, 3 → `prod`). Don't try to set it directly; set `replicas` instead.
- **Database `admin_password` not exposed by the resource.** The API stores it
  separately under Lake Secrets. To retrieve, use `GET /v1/db/<engine>/{id}/credentials`
  via CLI / API. A `ccp_db_<engine>_credentials` data source is on the roadmap.
- **Load balancer — `backend.scale_set_id` not yet supported.** Currently only
  `container_id` and `vm_instance_id` are valid backends. Scale-set-as-backend
  is on the roadmap.
- **Container scale set — no `ssh_key_ids` / `user_data` arguments.** Workaround:
  bake credentials into a `ccp_custom_template` first, then point the scale
  set at it. Native arguments are on the roadmap.
- **Reserved Terraform keywords.** Don't name attributes `count`, `for_each`,
  `provider`, `lifecycle`, `depends_on`, etc. — the schema crashes at load.
  We hit this with `ccp_vnet_ip_reservation.count` (renamed to `ip_count` in v0.7.1).

---

## Roadmap — v0.8.0 (next minor)

Aligning the schema with the API, based on gaps surfaced while building the
[`cetic-cloud-terraform-modules`](https://github.com/cetic-group/cetic-cloud-terraform-modules)
companion repo.

| Change | Resource / Datasource | Type | Why |
|---|---|---|---|
| Add `scale_set_id` to backend block | `ccp_load_balancer` | Schema | Today the LB can only reference individual instances. Scale-set-as-backend is the natural pattern for autoscaled tiers. |
| Add `ssh_key_ids` + `user_data` | `ccp_container_scale_set` | Schema | Match `ccp_container_instance` capabilities. Today scaling without a custom template means SSH-less replicas. |
| Add `ssh_key_ids` + `user_data` | `ccp_vm_scale_set` | Schema | Same as above. |
| Make `isolated` Optional+Computed | `ccp_vnet` | Schema | Allow toggling VNet isolation directly from Terraform (today: console / CLI only). |
| New datasource `ccp_db_pg_credentials` | DataSource | New | Expose `username` / `password` / `database` / `endpoint_host:port` for an instance. Same for `mysql` / `valkey` / `ferretdb`. |
| New datasource `ccp_db_valkey_credentials` | DataSource | New | Idem (Valkey: password only, no username). |
| New datasource `ccp_db_mysql_credentials` | DataSource | New | Idem. |
| New datasource `ccp_db_ferretdb_credentials` | DataSource | New | Idem. |
| Doc cleanup | `db_*_instance.md` | Doc | `tier` listed in "Attributes" (Computed), not "Required". |

## Roadmap — v1.0 (stable)

Stabilization milestone. Once we ship v1.0, the schema becomes a
backward-compatibility contract — only **additive** changes thereafter.

- Acceptance tests on a dedicated `tf-acc` org (full CRUD coverage).
- Schema review for breaking renames (e.g. `ccp_lxc_templates` → `ccp_container_templates`,
  `ccp_qemu_templates` → `ccp_vm_templates` to drop infra jargon from the
  public surface).
- Generate `docs/` from schema descriptions via
  [`tfplugindocs`](https://github.com/hashicorp/terraform-plugin-docs) so the
  doc never drifts from the code.
- Move catalog data sources behind a `ccp_template` unified abstraction.

---

## Testing

- **Unit tests** — `go test ./internal/client/`. To be added: HTTP roundtripper
  fakes for the `internal/client` package, covering happy path + the two error
  shapes (string vs list).
- **Acceptance tests** — `TF_ACC=1 go test ./internal/resources/...`. To be
  added: needs a real CETIC Cloud API endpoint + `CCP_API_KEY` in CI. Recommend
  running these against a dedicated `tf-acc` org so cleanup is straightforward.
- **Consumer modules** — see
  [`cetic-cloud-terraform-modules`](https://github.com/cetic-group/cetic-cloud-terraform-modules).
  Run `make test` there for `terraform test` on every module (uses
  `mock_provider` so no real API hit). Useful as a regression suite when
  evolving the schema.

---

## Release process

Set up since v0.5. Tag → goreleaser → Terraform Registry pickup (~5 min).

```bash
# After merging a feature/fix PR on main:
git pull origin main

# Bump version references in docs/index.md + README.md to the target version
sed -i 's|version = "~> 0\.7\.1"|version = "~> 0.8.0"|g' docs/index.md README.md
git add docs/index.md README.md
git -c user.email="<email>" commit -m "docs: bump provider version references to v0.8.0"
git push origin main

# Tag annotated + push
git tag -a v0.8.0 -m "v0.8.0 — <résumé des changements>"
git push origin v0.8.0

# Watch goreleaser GitHub Action
gh run watch
```

**Convention** (from `CLAUDE.md`): every tag MUST update :
1. `docs/index.md` — `required_providers` example.
2. `README.md` — Quick start example, status banner.
3. `docs/resources/*.md` and `docs/data-sources/*.md` — schema fidelity (Required / Optional / Computed) + example HCL using current field names.
4. After release, bump `cetic-cloud-terraform-modules` versions.tf in a follow-up PR.

### SemVer policy

- **Major** (`v1.0.0`): breaking schema change (rename / removal of an existing field).
- **Minor** (`v0.8.0`): new Optional field, new resource, new datasource.
- **Patch** (`v0.7.2`): bug fix, doc fix, internal refactor.
