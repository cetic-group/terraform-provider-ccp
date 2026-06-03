# Changelog

All notable changes to the CETIC Cloud Platform Terraform provider are
documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [4.2.1] — unreleased

### Added — header `X-CCP-Client: terraform` sur toutes les requêtes API

Toutes les requêtes HTTP émises par le provider portent désormais le header
`X-CCP-Client: terraform`. La plateforme l'utilise pour classer l'origine des
actions dans son journal d'activité de façon **déterministe** (jusqu'ici le
backend devait deviner via le User-Agent `terraform-provider-ccp/*`, qui
reste envoyé en complément).

## [4.2.0] — 2026-06-02

### Added — `ccp_public_ip` data source can now look up an IP by `label`

The `ccp_public_ip` data source can now look up an IP by `label` (in addition
to `id` / `ip_address`). Provide exactly one of the three. Ambiguous labels
(matching more than one public IP) raise an explicit error directing you to use
`id` or `ip_address` instead.

## [4.1.4] — 2026-06-02

### Fixed — AppGW : les Read utilisaient des endpoints GET inexistants (HTTP 405)

The API only exposes **list** endpoints for AppGW sub-resources — there is no
single-entity GET. Three client functions called non-existent endpoints, breaking
`terraform plan`/`refresh` as soon as the resources existed in state:

- `GetAppGWTargetGroup` called `GET /target-groups/{id}` → **405**. Now lists and
  filters client-side (same pattern as listeners).
- `ListAppGWTargetGroupMembers` called `GET /target-groups/{id}/members` (does not
  exist). Members are embedded in the target-group list response — now resolved
  from there.
- `GetAppGWRoute` called `GET /routes/{id}` → **405**. Now lists and filters.

### Known gap (not fixed here)

- `PATCH /target-groups/{tg_id}/members/{member_id}` does not exist API-side:
  updating a member `weight`/`enabled` in place will fail with 405. Workaround:
  taint the member to force a replace. Tracked for a follow-up (either API endpoint
  or RequiresReplace on those attributes).

## [4.1.3] — 2026-06-02

### Fixed — `ccp_appgw_target_group_member` : "Provider produced inconsistent result after apply" (.target_group_id)

- Same class of bug as v4.1.2 (vpc_id), on the member resource: the API response
  (`AppgwTargetGroupMemberResponse`) does **not** include `target_group_id`, and the
  provider overwrote the configured value with an empty string after create/read.
- `applyToModel` now preserves the configured/known `target_group_id` when the API
  omits it. Regression test added.
- With this fix, the full AppGW chain (gateway → listeners → target groups → members
  → routes) applies cleanly end-to-end.

## [4.1.2] — 2026-06-02

### Fixed — `ccp_application_gateway` : "Provider produced inconsistent result after apply" (.vpc_id)

- The platform API response (`AppGwResponse`) does **not** include `vpc_id`. The provider
  unconditionally mapped that (absent) field onto the Terraform state, overwriting the
  configured value with an empty string — every `terraform apply` creating a gateway
  failed with *Provider produced inconsistent result after apply: .vpc_id was X, but now ""*
  (the gateway itself WAS created server-side).
- `applyToModel` now preserves the configured/known `vpc_id` when the API omits it.
- Test fixtures no longer fake a `vpc_id` in API responses (they now mirror the real
  contract), and a regression test covers the preserve-on-omit behaviour.

## [4.1.1] — 2026-06-02

### Fixed — `ccp_application_gateway.plan` : validation client-side supprimée

- The hardcoded `OneOf("small", "medium", "large")` validator on
  `ccp_application_gateway.plan` rejected every plan key the live API
  actually accepts (`appgw-small`, `appgw-medium`, `appgw-large` — DB-driven
  via `compute_plans`, kind=`appgw`). Net effect: **no application gateway
  could be created through Terraform at all** (client-side validation and
  server-side validation had an empty intersection).
- The client-side validator is removed entirely: plan keys are now validated
  **server-side only**, against the live plan catalog. Backoffice-defined
  plans are accepted without a provider release (same philosophy as
  containers / VMs since platform v1.7.0).
- Docs + example updated to use the canonical `appgw-*` keys.

## [4.1.0] — 2026-06-01

### Added — `ccp_public_ip` label + description

- **`ccp_public_ip.label`** (Optional, max 100 chars) and
  **`ccp_public_ip.description`** (Optional) — client-facing annotations,
  mutable in-place via the backend PATCH endpoint (no detach / no replace).
- The `ccp_public_ip` **data source** now exposes `label` and `description`.
- Docs: allocating several IPs at once is done with Terraform's native
  `count` meta-argument (example added). The CLI (`cetic ip allocate
  --quantity`) and the `network/public-ip` module (variable `quantity`)
  wrap the same capability.

### Added — Let's Encrypt (ACME) on `ccp_load_balancer` + `ccp_appgw_listener`

- `ccp_load_balancer` listeners now support `protocol = "https"` with
  automatic Let's Encrypt certificates: `domain`, `acme_challenge`
  (`http01` | `dns01`), `acme_dns_provider`, `acme_dns_credentials`
  (sensitive), plus `health_check_enabled` / `health_check_path` and the
  computed `acme_status` / `acme_last_error`.
- `ccp_appgw_listener` now supports `acme_challenge`, `acme_dns_provider`,
  `acme_dns_credentials` (sensitive) — without `acme_challenge` the backend
  never issues a certificate.
- New data source **`ccp_acme_dns_providers`** — catalog of supported
  DNS-01 providers and their credential fields.

### Fixed — `ccp_load_balancer` / `ccp_appgw_listener` schema corrections

The previous listener implementations were **non-functional** (they called
endpoints that do not exist and sent fields the API ignores), so these
corrections are shipped as fixes rather than a major bump:

- LB listeners are now sent in the initial `POST /v1/load-balancers` call
  (the API does not support adding listeners afterwards — any listener
  change now forces LB replacement). Backends remain reconcilable in-place.
- LB listener fields renamed/aligned: `frontend_port` → `listen_port`,
  algorithm values are `roundrobin` | `leastconn` | `source`.
- **Removed** `ccp_load_balancer` `listener.name` and `backend.scale_set_id`
  (never existed backend-side).
- **Removed** `ccp_appgw_listener.custom_domain` (silently ignored by the
  API — use `acme_challenge = "dns01"` for customer-owned domains).

## [0.22.0] — 2026-05-26

### Added — `ccp_k8s_cluster` data source

- **`data "ccp_k8s_cluster"`** — new data source to look up an existing
  CETIC Cloud Kubernetes (CCKS) cluster by `id` or by the unique
  `(name, region)` pair. Backfills the missing read-side counterpart of
  the `ccp_k8s_cluster` resource (every other resource in the provider
  already had one).
- Exposes every Computed field of the resource counterpart, including
  the v0.21.0 additions:
  - `tier` — `dev` / `prod` HA topology selector.
  - `proxy_secondary_vmid`, `proxy_secondary_node`, `proxy_vip_vnet` —
    read-only HA proxy fields (null for `tier = "dev"`).
- Lookup discriminator semantics match the rest of the provider:
  exactly one of `id` xor `(name + region)` must be set; conflicting
  combinations or no-match / multiple-match results raise an explicit
  diagnostic instead of silently picking a row.

### Changed

- All `~> 0.21.0` version pins in `README.md` and `docs/index.md`
  bumped to `~> 0.22.0`. New `docs/data-sources/k8s_cluster.md` with
  the standard `Example Usage` / `Argument Reference` /
  `Attributes Reference` sections and a side-by-side example for both
  lookup modes.

### Backend dependency

- No new CCP API dependency — the data source reuses
  `GET /v1/k8s/clusters` and `GET /v1/k8s/clusters/{id}`, both already
  consumed by the resource. Backward-compatible with any CCP API
  version that already supports the resource.

## [0.21.0] — 2026-05-26

### Added — CCKS HA tier (`dev` / `prod`)

- **`ccp_k8s_cluster.tier`** — new Optional+Computed attribute (default
  `"dev"`) controlling the LXC proxy topology fronting the apiserver:
  - `dev` — single LXC proxy (SPOF acceptable in dev/staging — current
    behaviour, preserved as the default for backward compatibility).
  - `prod` — 2 LXC proxies (primary + secondary) with Keepalived VRRP
    and a floating VIP, providing HA at the proxy layer.

  Validators enforce `OneOf("dev", "prod")`. The attribute is immutable —
  changing the tier carries `RequiresReplace()` because the proxy LXC
  topology is baked at provision time on the backend.
- **`ccp_k8s_cluster.proxy_secondary_vmid`** — new Computed (read-only)
  Int64 attribute exposing the Proxmox VMID of the secondary LXC proxy.
  Null for `tier = "dev"`.
- **`ccp_k8s_cluster.proxy_secondary_node`** — new Computed (read-only)
  String attribute exposing the Proxmox node hosting the secondary LXC
  proxy. Null for `tier = "dev"`.
- **`ccp_k8s_cluster.proxy_vip_vnet`** — new Computed (read-only) String
  attribute exposing the Keepalived VRRP floating VIP shared between the
  LXC proxies. Null for `tier = "dev"`.

### Changed

- All `~> 0.20.0` version pins in `README.md` and `docs/index.md`
  bumped to `~> 0.21.0`. New HCL example added to
  `docs/resources/k8s_cluster.md` showcasing `tier = "prod"`.

### Backend dependency

- Requires CCP API ≥ v2.6.9 (`tenant_k8s_clusters.tier` SAEnum +
  `proxy_secondary_*` + `proxy_vip_vnet` exposed on `GET /v1/k8s/clusters/{id}`).

## [0.20.0] — 2026-05-23

### Added — SSH key scope (visibility levels)

- **`ccp_ssh_key.scope`** — new Optional+Computed attribute (default
  `"user"`) controlling who can see an SSH key once it is registered:
  - `user` — visible only to its creator, survives org switches.
    Any organisation member can create.
  - `org` — visible inside the currently active organization only.
    Org `admin`+ or `owner` required to create.
  - `tenant` — visible across every org of the tenant and every invited
    member. Tenant `owner` only.

  Validators enforce `OneOf("user", "org", "tenant")`. The attribute is
  immutable — changing the scope carries `RequiresReplace()` because the
  CETIC Cloud API has no PATCH endpoint for SSH keys.
- **`ccp_ssh_key.created_by_tenant_id`** — new Computed (read-only)
  attribute exposing the UUID of the tenant the key was created from.
  Null on legacy keys predating the scoping migration.

### Changed

- All `~> 0.19.0` version pins in `README.md` and `docs/index.md`
  bumped to `~> 0.20.0`. New HCL examples added to
  `docs/resources/ssh_key.md` for `scope = "org"` and `scope = "tenant"`.
- `internal/client/types.go::SSHKey` gains `Scope string` and
  `CreatedByTenantID string` fields with `omitempty` JSON tags;
  `SSHKeyCreateRequest` gains an optional `Scope string` (omitted when
  empty so the API default applies — keeps the wire format backwards
  compatible).

### Notes

- Pairs with the backend migration that adds
  `ssh_keys.scope ENUM('user','org','tenant') NOT NULL DEFAULT 'user'`
  and `ssh_keys.created_by_tenant_id UUID NULL`.
- Backward-compatible — existing HCL with no `scope` declaration keeps
  working: Terraform materialises the default `"user"` into state on
  the next apply. No drift, no destroy/recreate of existing keys.
- The TF Modules repo will bump its provider constraint to `>= 0.20.0`
  in its next release.

## [0.19.0] — 2026-05-21

### Added — AppGW route `strip_prefix`

- **`ccp_appgw_route.strip_prefix`** — new Optional+Computed attribute
  (default `false`). When `true` and `path_match` is non-empty in
  `prefix`/`exact` mode, the gateway strips the `path_match` prefix from
  the request URL before forwarding to the backend. E.g. with
  `path_match = "/web-app"` and `strip_prefix = true`, an incoming
  request to `/web-app/foo` reaches the backend as `/foo`. Ignored when
  `path_match` is empty or when `path_match_type = "regex"`.
- `internal/client/appgw_types.go::AppGWRoute` gains a `StripPrefix bool`
  field; `AppGWRouteCreateRequest` / `AppGWRouteUpdateRequest` gain an
  optional `StripPrefix *bool` (omitted when nil so the API default
  applies / a PATCH leaves the value untouched).

### Changed

- All `~> 0.18.0` version pins in `README.md` and `docs/index.md` bumped
  to `~> 0.19.0`. New HCL example added to `docs/resources/appgw_route.md`
  illustrating `strip_prefix = true`.

### Notes

- Pairs with the backend migration 171 (CCP) that adds
  `appgw_routes.strip_prefix BOOLEAN NOT NULL DEFAULT FALSE`.
- The TF Modules repo bumps its provider constraint to `>= 0.19.0` and
  exposes `strip_prefix` on `modules/atomic/appgw-route` plus the route
  schema of `modules/managed/application-gateway` and
  `modules/exposure/web-app-with-appgw`.

## [0.18.0] — 2026-05-20

### Added — Load Balancer plan tiers

- **`ccp_load_balancer.plan`** — new Optional+Computed attribute (default
  `"small"`) that controls the capacity of the LB instance pair. Three
  tiers are exposed by the platform's pricing families v3:
  - `small`  — 1 vCPU / 512 MB —  4.99 €/month (default, current behaviour)
  - `medium` — 2 vCPU /   1 GB — 11.99 €/month
  - `large`  — 4 vCPU /   2 GB — 27.99 €/month

  Validators enforce `OneOf("small", "medium", "large")`. The attribute is
  immutable — changing the plan carries `RequiresReplace()` since the
  platform does not support in-place resizing of the LB pair (would
  require an LXC rebuild). Existing load balancers with no explicit `plan`
  in HCL continue to default to `small`, matching the API default — no
  drift, no breaking change.

### Changed

- All `~> 0.16.0` / `~> 0.17.0` version pins in `README.md`, `docs/index.md`
  and `examples/loadbalancer/main.tf` bumped to `~> 0.18.0`. The
  `ccp_load_balancer` example now sets `plan = "medium"` to demonstrate
  the new attribute.
- `internal/client/types.go::LoadBalancer` gains a `Plan string` field;
  `LoadBalancerCreateRequest` gains an optional `Plan string` (omitted
  when empty so the API default applies).

### Notes

- This release pairs with the backend pricing-families-v3 work (LB
  `compute_plans` + monthly pricing rows for the three tiers).
- The TF Modules repo bumps its provider constraint to `>= 0.18.0` and
  surfaces a new `plan` variable on `modules/exposure/load-balancer`
  (cf. modules `v0.12.0`).

## [0.17.0] — 2026-05-17

### Added — Support plans

- **`ccp_support_subscription`** resource — singleton-per-tenant
  managing the current support plan (Create = `POST /v1/support/subscribe`,
  Delete = `POST /v1/support/unsubscribe` which downgrades to the
  baseline). Read reflects whatever subscription is currently active
  on the tenant. Cf. CCP backend wave C6.
- **`ccp_support_plan`** data source — exposes `display_name`,
  `price_eur_month{,_cents}`, `sla_first_response_hours`,
  `sla_resolution_hours`, `max_priority`, `channels`, `is_default`,
  `is_active`, `features` for any of the published plans.

## [0.16.0] — 2026-05-16

### Added — Billing v2

- `ccp_pricing` data source for the live tariff catalog (`compute_plans`,
  `db_plans`, public IPs, storage, network egress, …) — single round-trip,
  no hard-coded prices in HCL.
- `ccp_promo_codes_available` data source listing promo codes the caller
  is eligible for.
- `ccp_budget` resource — monthly cap in cents + alert thresholds + hard
  stop at 100%.
- `ccp_commit` resource — yearly / monthly engagement with the platform's
  discount tiers (-10% monthly, -20% yearly).

## [0.15.0] — 2026-05-16

### Added — Application Gateway L7 polish

- **`ccp_application_gateway.public_ip_status`** — new Computed attribute
  exposing the lifecycle of the public IP attachment (`allocated` |
  `attaching` | `attached` | `detaching` | `error`, null when no IP is
  attached). Mirrors the standard `public_ip_status` helper already
  shipped on `ccp_load_balancer`, `ccp_vm_instance` and
  `ccp_container_instance` (cf. the platform `public_ip` UX convention,
  2026-05-02). The provider polls this field during apply so attach /
  detach blocks until the asynchronous IPaaS pipeline has converged.
- **`ccp_application_gateway.public_ip_address`** — new Computed
  attribute mirroring the IPv4 string currently bound to the gateway.
  Saves users a `data "ccp_public_ip"` round-trip when they just need
  the address for an output / DNS record.
- **Data source `ccp_application_gateway`** — same two attributes
  surfaced for read-only lookups.

### Fixed

- **`ccp_appgw_route.basic_auth_user.user`** — renamed nested block
  attribute from `username` to `user` and aligned the JSON wire payload
  with the backend Pydantic schema (`AppgwBasicAuthUser.user`). The
  previous `"username"` key was silently dropped by Pydantic
  (`extra=ignore`), the required `user` field was missing, and every
  `POST /v1/app-gateways/{id}/routes` carrying `basic_auth_users`
  would have returned 422. **Breaking change for HCL** that declared
  `basic_auth_user { username = ... }` — rename the attribute to
  `user` and re-apply. No state migration needed: the platform never
  stores plaintext (only `basic_auth_secret_ref`), so re-applying the
  same user/password pair is a no-op server-side.
- **`AppGwStatus` enum** — removed the `updating` value from
  `internal/client/appgw_types.go`. The backend pipeline was
  reworked in v1.8.x to apply PATCHes in place and keep the row
  `active` throughout. No Go code branched on `AppGWStatusUpdating`
  but the constant and doc strings (`status`: "creating | active |
  updating | error | deleting") implied otherwise. Now aligned with
  the backend `AppGwStatus` literal: `creating | active | error |
  deleting`. The `pollUntilReady` loop continues to wait through
  transient intermediate values regardless.

### Changed

- `basic_auth_user` semantics clarified in the resource docs: omitting
  the block on update preserves the existing configuration; passing an
  empty list explicitly clears it. The backend
  `_resolve_basic_auth_ref` helper already implements those three
  cases — the docs now match.
- Added wire-format guards in
  `internal/resources/appgwroute/appgwroute_test.go::TestCreateRoute_SendsAllFields`
  to assert `"user":"admin"` is on the wire and the legacy `username`
  key never leaks back in (regression guard against the JSON-tag bug
  fixed above).

### Notes

- The provider audit also surfaced a mismatch on `ccp_appgw_listener`:
  the Go schema exposes `custom_domain` but the backend
  `AppgwListenerCreate` schema has no such field, while ACME-related
  fields (`acme_challenge`, `acme_dns_provider`,
  `acme_dns_credentials`) supported by the backend are not yet
  represented. Tracked as a separate gap — out of scope for this
  release since it touches the immutable listener identity surface
  and warrants its own design pass (Sensitive nested attribute for
  the DNS credentials, `acme_status` polling, dropdown of providers
  surfaced via `GET /v1/appgw/acme-dns-providers`).

## [0.14.0] — 2026-05-15

### Added — Application Gateway v1 (L7)

- **`ccp_application_gateway`** resource — manages a CETIC Cloud
  Application Gateway (`ccp-appgw`), an L7 HTTP/HTTPS reverse proxy with
  TLS termination, SNI multi-cert, rate limiting, IP allow/deny and WAF
  presets. Each gateway is a highly available pair with a floating
  virtual IP and automatic failover. `region` / `vpc_id` / `vnet_id`
  are immutable; `plan`, public IP attachment, `force_https`, HSTS
  settings, global rate limit and CIDR allow/deny lists are mutable in
  place. The provider polls until status reaches `active` (typically
  3-5 minutes for the initial create).
- **`ccp_appgw_listener`** resource — one hostname + Let's Encrypt cert
  per listener. `hostname` and `custom_domain` are immutable (force
  replacement) to avoid leaving stale certs lying around.
- **`ccp_appgw_target_group`** resource — a backend pool with
  load-balancing algorithm (`roundrobin` / `leastconn` / `source`) and
  L7 health-check configuration. Cookie-based sticky sessions supported
  via `sticky_enabled` / `sticky_cookie_name`.
- **`ccp_appgw_target_group_member`** resource — a single backend
  inside a target group. Exactly one of `container_id`,
  `vm_instance_id` or `target_ip` must be set, enforced at plan-time
  via `ValidateConfig` (early-returns on Unknown to avoid spurious
  errors during `terraform validate`).
- **`ccp_appgw_route`** resource — one L7 routing rule (path + headers
  + methods → target group) with per-route policies: rate limit,
  IP allow/deny, CORS, basic auth (`basic_auth_user.password` is
  Sensitive), WAF preset (`off` / `permissive` / `strict`), request
  and response header injection. Routes are evaluated in ascending
  `priority` order — the first match wins.
- **`ccp_application_gateway`** data source — look up an existing
  gateway by `id` or by `(name, region)`. Returns the full gateway
  plus a read-only summary of attached listeners / target groups /
  routes for inventory queries.

### Notes

- The 5 resources are split (rather than nesting listeners and routes
  inside the gateway) to keep per-route HCL edits idempotent — a
  rate-limit tweak on `ccp_appgw_route.api_v1` PATCHes that one row
  without re-validating the entire gateway.
- `basic_auth_user` is a nested block of `{username, password}` —
  passwords are hashed server-side into a Secret Manager entry
  (`basic_auth_secret_ref`). After `terraform import`, the
  `basic_auth_user` blocks are empty in state; re-running `terraform
  apply` with the expected users reconciles.
- Anti-pattern guard rails from `CLAUDE.md` were respected:
  no `ModifyPlan` on `Required` attributes; FKs (`appgw_id`,
  `listener_id`, `target_group_id`) carry `RequiresReplace`; nested
  block helpers (`applyToModel`) preserve plan-side intent for fields
  the API never echoes back (passwords).

## [0.13.0] — 2026-05-13

### Added — Secret Manager v1

- **`ccp_secret`** resource — manages an encrypted key/value blob in the
  CETIC Cloud Secret Manager. `data` is `Sensitive` and persisted in the
  Terraform state (the API never re-emits plaintext outside the
  audit-logged reveal endpoint, so drift on `data` is **not** detected by
  the provider). `name` is immutable (force replacement); `description`
  and `tags` are mutable in place via `PATCH`; changing `data` triggers
  a server-side rotation (`POST /v1/secrets/{id}/rotate`) and bumps the
  `version` counter. Secrets are K8s-type agnostic — the type is
  specified in the `CCPSecret` CRD when syncing to K8s.
- **`ccp_secret`** data source — looks up secret metadata by `id` or
  `name`. **Never returns plaintext `data`** — data sources never reveal
  secrets on this platform.

### Changed

- Unified `tags` attribute (was `labels` map) to align with other
  resources (`ccp_object_bucket`, `ccp_vm_instance`, `ccp_block_volume`,
  …). Tags are now a `list(string)` (e.g. `["env:prod", "team:platform"]`)
  rather than a `map(string)`.

## [0.11.1] — 2026-05-12

### Fixed

- **`ccp_registry` ValidateConfig** — the `at least one exposure must be
  enabled` check fired spuriously during `terraform validate` of any
  consumer (modules, landing-zones) that omitted `expose_public` /
  `expose_private` to take the defaults. Both attributes are
  Optional+Computed with `booldefault.StaticBool(...)` — at validate-time
  (before PlanModifiers apply), the framework surfaces them as Unknown.
  The validator now skips when either value is Null or Unknown; plan-time
  enforcement (resource logic) and the API CHECK constraint remain in
  place. Affects callers of `cetic-cloud-terraform-modules` v0.5.0+.

## [0.11.0] — 2026-05-11

### Added — IAM Roles v1

- **`ccp_iam_role`** resource — manages a custom IAM role with an
  AWS-style policy document. `policy_document_json` accepts any JSON
  produced by the `ccp_iam_policy_document` data source or hand-rolled.
  Computed `is_built_in` (always `false` for resource-managed roles) and
  `policy_hash` (SHA-256 canonicalized — used for drift detection across
  re-applies). Mutable in place: `name`, `description`, `policy_document_json`.
- **`ccp_iam_role_assignment`** resource — attaches a role to a
  principal (`org_member`, `api_key`, `service_account`, `ccks_workload`).
  All attributes force replacement (revoke + reassign for changes).
  Optional `expires_at` (ISO 8601) auto-revokes after the deadline.
- **`ccp_service_account`** resource — machine identity with rotating
  token. `token` returned ONCE at Create (Sensitive, captured into state
  and explicitly preserved by Read). Same pattern as `ccp_api_key.token`
  and `ccp_registry_user.password`. To rotate, taint the resource.

### Added — data sources

- **`ccp_iam_role`** — look up an existing role by `id` or by `(name,
  built_in)`. Built-ins (`AdminAll`, `ReadOnlyAll`, `Member`,
  `RegistryAdmin`, `RegistryReader`, `BucketReader`, `BucketWriter`,
  `K8sViewer`, `BillingReader`, `NetworkAdmin`) are looked up via
  `built_in = true`.
- **`ccp_iam_policy_document`** — AWS-style helper that transforms
  repeatable `statement {}` blocks (with `effect`, `actions`,
  `resources`, optional `condition` and `sid`) into a canonicalized
  JSON string ready to pass to `ccp_iam_role.policy_document_json`. Pure
  HCL→JSON transformation, no API call.

### Added — internals

- New `internal/client/iam.go` + `iam_types.go` covering the 13
  `/v1/iam/*` endpoints (roles CRUD, role assignments CRUD, principals
  effective-permissions, simulate, resources/actions catalogs).
- New `internal/client/serviceaccount.go` + types for the 6
  `/v1/service-accounts/*` endpoints (CRUD + rotate).
- New `internal/iamarn` package — Go port of the API ARN parser/matcher
  (`apps/api/app/services/iam_arn.py`). 75 cases of the Python test
  suite re-implemented in Go (`iamarn_test.go`) for parity. Use this
  package for any pattern validation client-side.

### Changed

- All `~> 0.10.0` version pins in README, docs/index.md, and examples
  bumped to `~> 0.11.0`.

### Notes

- **Cohabitation with legacy RBAC** — `owner` / `admin` / `member` /
  `viewer` org-member roles continue to work without explicit IAM
  assignments (short-circuit in the API's `require_permission`). IAM
  assignments add fine-grained permissions on top.
- **Cross-tenant isolation** — `ccp_iam_role` policy documents may only
  reference ARNs from the caller's tenant or wildcard `*`. The API
  rejects any cross-tenant ARN at Create/Update time.
- **Self-elevation interdiction** — custom roles cannot contain
  `iam:AttachRole`, `iam:CreateRole`, `iam:UpdateRole`, `iam:DeleteRole`,
  `iam:DetachRole` actions. Only `iam:Get*`, `iam:List*`,
  `iam:Simulate*`, `iam:GetEffectivePermissions` are allowed in custom
  roles. The built-in `AdminAll` is the only role with full `iam:*`.

## [0.10.0] — 2026-05-09

### Added — CETIC Container Registry (CCR, Phase 6)

- **`ccp_registry`** resource — manages a per-tenant Distribution-based
  registry (Traefik + `registry:2.8` + cesanta/docker_auth in an LXC
  container). Supports both `public` (HTTP-01 Let's Encrypt) and `private`
  (DNS-01 IONOS) exposure modes. Provisioning is asynchronous; the
  provider polls until the registry reaches `active` status with a 20 min
  budget (DNS-01 propagation can stretch a few minutes past TLS issuance).
  - One-shot `admin_password` returned by Create — captured into state,
    never re-emitted by the API. Same pattern as `ccp_api_key.token`. To
    rotate, taint the resource.
  - Mutable in place: `name`, `gc_schedule_cron`, `image_tag`, `tags`,
    `public_ip_id` (attach/detach via dedicated endpoints).
  - `image_tag` is `Optional + Computed` with `UseStateForUnknown` —
    omitting it lets the platform default propagate (currently `2.8`)
    without diff drift on subsequent applies.
- **`ccp_registry_user`** resource — non-admin users for `docker login`.
  `password` is delivered ONCE at Create() and explicitly preserved during
  Read() (the API never re-emits it). All attributes force replacement;
  rotate password by tainting.
- **`ccp_registry_acl`** resource — grants users `pull`/`push`/`*` actions
  on a `repo_pattern`. Update is supported in place (PATCH).
- **`ccp_registry`** data source — look up an existing registry by `id`
  or by `(name, region)`.

### Added — internals

- New `internal/client/registry.go` + `registry_types.go` covering the 14
  `/v1/registries/*` methods.
- New `internal/client/testutil` package — the first shared test helper of
  the repo, providing `NewTestServer(t, Routes)` for unit tests against a
  mocked HTTP backend. Supports stateful BodyFn fixtures, request
  recording, and pending-route tracking.
- First `*_test.go` files of the repo, exercising the four new
  resources/data source against `httptest.Server`. Will serve as the
  template for back-filling tests on existing resources.

### Changed

- README and `docs/index.md` examples now reference `version = "~> 0.10.0"`.
- `docs/ADDING_RESOURCE.md` backlog: registry trio marked implemented.

### Notes

- Workload identity for in-cluster pulls (CCKS) is **not** exposed via
  Terraform — it is handled transparently by the cluster-agent through
  SA-token exchange against `/v1/auth/ccks-registry-exchange`.
- Custom domain support is backlogged — v1 only exposes `<slug>.cloud.cetic-group.com`.

[0.10.0]: https://github.com/cetic-group/terraform-provider-cetic-cloud-platform/releases/tag/v0.10.0
