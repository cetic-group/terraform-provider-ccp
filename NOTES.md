# Cloud Lake Terraform Provider — Notes & Roadmap

Internal notes for contributors. Audience: the next dev picking this up.

---

## What's implemented in v0.3

- Provider config (`api_key` / `endpoint`, both env-overridable via
  `CCP_API_KEY` / `CCP_API_URL`).
- `ccp_ssh_key` — create / read / delete. No update.
- `ccp_vpc` — create / read / delete, async with status polling.
- `ccp_vnet` — create / read / update (name, snat) / delete, async.
- `ccp_container_instance` — LXC. Create / read / delete with status
  poll until `running` + IP resolved. All fields RequiresReplace (no PATCH).
- `ccp_block_volume` — Ceph RBD. Create / read / update (`size_gb`
  grows + attach/detach via `attached_to_*`) / delete. Auto-detach before delete.
- `ccp_public_ip` — Allocate / read / update (attach/detach via
  `attached_to_*`) / release. Sync allocate, attach can be async on IPaaS.
- `ccp_object_bucket` — Ceph RGW S3. Create / read / update (`is_public`)
  / delete. Master S3 creds (`access_key`, `secret_key`) fetched after
  `active` and stored in state, marked sensitive.
- `ccp_vm_instance` — QEMU VM. Create / read / update (name, tags)
  / delete. Async, polls until `running` (up to 10 min).
- `ccp_regions` data source — lists active regions.
- `ccp_organizations` data source — lists orgs accessible to the
  current API key's tenant.

---

## Architecture

**Plugin Framework, not SDK v2.** The provider is built on
`terraform-plugin-framework` (Hashicorp's current generation). Don't pull in
`terraform-plugin-sdk/v2` even for "simpler" resources — keeping the code on
one framework matters for diagnostics and schema consistency.

**Hand-written HTTP client at `internal/client/`.** It's a thin wrapper around
`net/http` with typed request/response structs per endpoint. No OpenAPI codegen.
Reasons: the Cloud Lake API spec changes frequently while features land, and a
hand-rolled client lets us absorb the inconsistencies (see "API caveats" below)
in one place rather than fighting a generator.

**Auth — `ccp_live_*` API keys via Bearer header.** The Cloud Lake API also
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
production API on `api.in.techledger.io`.

- **SSH keys — no GET single endpoint.** `GET /v1/ssh-keys/{id}` doesn't exist.
  The client does `GET /v1/ssh-keys` and filters client-side by ID. Fine for
  small fleets, will need pagination later.
- **SSH keys — no Update endpoint at all.** `PATCH` / `PUT` are not implemented
  server-side. Every attribute is therefore `RequiresReplace`.
- **VPC — SDN type is a string, not a validated enum.** The API returns
  `sdn_type: "vxlan"` or `"simple"` as a free-form JSON string. No `oneOf` in
  the OpenAPI schema. We type it as `types.String` and don't validate.
- **VNet — also no GET single endpoint.** Same trick: list under the parent VPC
  and filter by ID.
- **VNet — PATCH is partial.** The API only accepts `name` and `snat` in
  PATCH. Everything else (CIDR, VPC, region…) is `RequiresReplace`.
- **VPC + VNet — async provisioning.** POST returns 201 immediately with
  `status: "provisioning"`. The provider polls until the status reaches
  `active` (success), `error` (fail), or `deleting` (during destroy). Worst
  case observed: ~90s on first VPC create when a NAT gateway LXC needs to be
  spawned.
- **Errors — FastAPI shape, sometimes a list.** Most errors come back as
  `{"detail": "human string"}`, but Pydantic validation errors return
  `{"detail": [{"loc": [...], "msg": "...", "type": "..."}]}`. The client
  normalises both into a single error string.
- **Error messages are in French.** Cloud Lake is a French/CI product and the
  API speaks French. The provider passes messages through unmodified — don't
  try to translate them.
- **Container — no Update endpoint.** All attributes are RequiresReplace. The
  only mutable state is via `/actions` (start/stop/restart) which the provider
  doesn't expose as Terraform attributes (state transitions are runtime, not
  config).
- **Container — IP resolution lags.** `ip_address` is null in the 201 response
  and gets filled by Celery once Proxmox DHCP returns. The provider polls until
  the container is `running` AND `ip_address` is non-null (or up to 5 min, then
  warns and continues with a refresh hint).
- **Block volume — attach is on a separate endpoint.** No `attached_to_*` on
  POST/PATCH. Provider models attachment as Terraform attributes
  (`attached_to_id` + `attached_to_type`) and translates plan/state diffs into
  attach/detach API calls.
- **Block volume — `size_gb` only grows.** `POST /resize` accepts only
  increases. Provider enforces this in `Update` and emits a clear diagnostic
  on shrink attempts.
- **Block volume — must be detached before delete.** Provider auto-detaches
  before calling DELETE so users don't have to pre-detach in HCL.
- **Public IP — no GET single endpoint.** Same list+filter trick as SSH keys
  and VNets.
- **Public IP — attach is conditional sync/async.** Demo pools attach
  synchronously. IPaaS routed pools dispatch a Celery task — provider polls
  until `status: attached`.
- **Public IP — `load_balancer_id` is computed.** LB attach uses a different
  endpoint (`POST /v1/load-balancers/{id}/attach-ip`), not `attach`. The
  `attached_to_type` schema validator therefore only accepts
  `container` / `vm_instance`.
- **Object bucket — credentials are tenant-region-wide.** `GET
  /v1/buckets/{id}/credentials` returns the **master** S3 key, which covers
  every bucket the tenant has in that region — not just the bucket the call
  was made on. Storing them in state (sensitive) is fine but be aware that
  rotating one key invalidates state for *all* buckets in that region.
- **Object bucket — `tags` is RequiresReplace.** The PATCH endpoint only
  accepts `is_public`. The provider therefore forces replacement on tag
  changes (which means destroy + create, losing data). Avoid evolving tags
  on critical buckets until the API gains tag mutability.
- **Object bucket — credentials fetch returns 409 if not `active`.** The
  provider only fetches them when status reaches `active`; otherwise the
  state keeps the previously known values (or empty on first apply).
- **VM instance — provisioning is slow.** Cloud-init + apt + qemu-guest-agent
  install can take 2-5 minutes on a fresh boot. The provider polls up to
  **10 minutes** for `running` + IP, vs 5 minutes for containers.
- **VM instance — PATCH is partial.** Only `name` and `tags` are mutable.
  Plan, region, template, vnet_id are RequiresReplace.
- **VM instance — actions endpoint not exposed.** Start/stop/shutdown/reboot
  are runtime state transitions, not config. The provider doesn't expose
  them as resource attributes (they'd cause spurious diffs). Use `terraform
  taint` + apply, or `lake vm action` from the CLI for state transitions.

---

## Roadmap — next resources to add

Based on the catalog in the project root `CLAUDE.md`. Estimates are rough LOC
counts (Go, schema + CRUD + plan modifiers, no tests).

| Resource                          | Phase | Notes                                                                | LOC est. |
|-----------------------------------|-------|----------------------------------------------------------------------|----------|
| `ccp_container_scale_set`   | P1+   | Autoscale group. Replicas, target tags, health policy.               | ~500 |
| `ccp_vm_scale_set`          | P3    | VM scale set (mirrors container scale set).                          | ~500 |
| `ccp_load_balancer`         | P4    | HAProxy + Keepalived pair. Listeners + backends nested.              | ~700 |
| `ccp_pg_instance`           | P5    | DBaaS PostgreSQL (CNPG).                                             | ~400 |
| `ccp_mysql_instance`        | P5    | DBaaS MariaDB.                                                       | ~400 |
| `ccp_redis_instance`        | P5    | DBaaS Valkey.                                                        | ~400 |
| `ccp_mongo_instance`        | P5    | DBaaS FerretDB.                                                      | ~400 |
| `ccp_k8s_cluster`           | P6    | CAPI / CAPMOX cluster.                                               | ~600 |
| `ccp_blockchain_network`    | P7    | Hyperledger Besu.                                                    | ~400 |

Suggested next: `ccp_load_balancer` (the linchpin for any production
deployment — wires containers/VMs together with an external entrypoint).

---

## Testing

Nothing runs in v0.1.

- **Unit tests** — `go test ./internal/client/`. To be added: HTTP roundtripper
  fakes for the `internal/client` package, covering happy path + the two error
  shapes (string vs list).
- **Acceptance tests** — `TF_ACC=1 go test ./internal/resources/...`. To be
  added: needs a real Cloud Lake API endpoint + `CCP_API_KEY` in CI. Recommend
  running these against a dedicated `tf-acc` org so cleanup is straightforward.

---

## Release process — to define

Not set up yet. Open TODOs:

- [ ] GitHub Actions workflow with `goreleaser` (cross-compile linux/darwin/windows × amd64/arm64).
- [ ] GPG-sign release artifacts (Terraform Registry requires a detached signature).
- [ ] Publish to the Terraform Registry under `cetic-group/cetic-cloud-platform`.
- [ ] Generate `docs/` from schema descriptions via
      [`tfplugindocs`](https://github.com/hashicorp/terraform-plugin-docs)
      and commit the output (the registry consumes them from there).
- [ ] Decide on a versioning policy — recommend pinning `0.x` until Phase 5
      lands, then bump to `1.0.0` once the surface stops moving.
