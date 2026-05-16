# Changelog

All notable changes to the CETIC Cloud Platform Terraform provider are
documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
