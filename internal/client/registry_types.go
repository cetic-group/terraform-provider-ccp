// Code for CETIC Container Registry (CCR) — Phase 6.
//
// CCR is a Distribution-based registry deployed in a per-tenant K8s
// namespace within the regional shared workload cluster. Tenant-side it
// has NO network resource (no VPC/VNet/Public IP) — exposure is via the
// 2 shared regional Gateways (`registry-gateway-public` /
// `registry-gateway-private` in `cilium-system`) controlled by the
// `expose_public` / `expose_private` toggles. A single hostname is
// served via DNS split-horizon:
//
//	`<slug>-<id8>.registry-<region>.cloud.cetic-group.com`
//
// resolved to the public Gateway IP from the Internet, or to the private
// Gateway IP from the CETIC LAN.
package client

import "time"

// ─── Registry ─────────────────────────────────────────────────────────────────

type Registry struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Region         string    `json:"region"`
	ExposePublic   bool      `json:"expose_public"`
	ExposePrivate  bool      `json:"expose_private"`
	URL            *string   `json:"url,omitempty"`
	ImageTag       string    `json:"registry_image_tag"`
	GCScheduleCron string    `json:"gc_schedule_cron"`
	Status         string    `json:"status"`
	StorageUsedGB  *int64    `json:"storage_used_gb,omitempty"`
	LastPushAt     *string   `json:"last_push_at,omitempty"`
	AdminUsername  *string   `json:"admin_username,omitempty"`
	ErrorMessage   *string   `json:"error_message,omitempty"`
	Tags           []string  `json:"tags"`
	CreatedAt      time.Time `json:"created_at"`
}

// RegistryCreateResponse is returned by POST /v1/registries.
//
// `admin_password` is delivered ONCE — same one-shot creds pattern as
// `ApiKeyCreateResponse.Token`. Callers MUST capture it before any Read()
// (the API never re-emits it).
type RegistryCreateResponse struct {
	Registry
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
}

type RegistryCreateRequest struct {
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	ExposePublic  bool     `json:"expose_public"`
	ExposePrivate bool     `json:"expose_private"`
	ImageTag      *string  `json:"image_tag,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// RegistryUpdateRequest — PATCH /v1/registries/{id}. Toggle exposure +
// edit tags. Name/cron/image not mutable post-creation in v1.
type RegistryUpdateRequest struct {
	ExposePublic  *bool    `json:"expose_public,omitempty"`
	ExposePrivate *bool    `json:"expose_private,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// ─── Registry users (token-based JWT auth via cesanta/docker_auth) ───────────

type RegistryUser struct {
	ID         string    `json:"id"`
	RegistryID string    `json:"registry_id"`
	Username   string    `json:"username"`
	IsAdmin    bool      `json:"is_admin"`
	CreatedAt  time.Time `json:"created_at"`
}

type RegistryUserCreateResponse struct {
	RegistryUser
	Password string `json:"password"`
}

type RegistryUserCreateRequest struct {
	Username string `json:"username"`
}

// ─── Registry ACLs ────────────────────────────────────────────────────────────

type RegistryACL struct {
	ID          string    `json:"id"`
	RegistryID  string    `json:"registry_id"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	RepoPattern string    `json:"repo_pattern"`
	Actions     []string  `json:"actions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RegistryACLCreateRequest struct {
	UserID      string   `json:"user_id"`
	RepoPattern string   `json:"repo_pattern"`
	Actions     []string `json:"actions"`
}

type RegistryACLUpdateRequest struct {
	RepoPattern *string  `json:"repo_pattern,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

// ─── Status constants ─────────────────────────────────────────────────────────

const (
	RegistryStatusCreating     = "creating"
	RegistryStatusProvisioning = "provisioning"
	RegistryStatusActive       = "active"
	RegistryStatusError        = "error"
	RegistryStatusDeleting     = "deleting"
)
