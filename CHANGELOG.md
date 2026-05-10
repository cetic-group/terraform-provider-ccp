# Changelog

All notable changes to the CETIC Cloud Platform Terraform provider are
documented in this file. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
the project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
