# Changelog

All notable changes to the CETIC Cloud Platform Terraform provider are
documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
