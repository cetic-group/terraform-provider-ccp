// IAM Roles v1 types — POJOs for `/v1/iam/*` and `/v1/service-accounts/*`.
//
// Mirrors the Pydantic schemas in `apps/api/app/schemas/iam.py`. Policy
// document version is figée at "2026-05-10".
package client

import (
	"encoding/json"
	"time"
)

// ─── Policy document ────────────────────────────────────────────────────────

// PolicyStatement matches schemas/iam.py::PolicyStatement.
//
// Condition is a free-form `{operator: {key: value|list[value]}}`. v1
// operators: StringEquals, StringNotEquals, StringLike, IpAddress,
// NotIpAddress, DateGreaterThan, DateLessThan. Keys: ResourceTag,
// RequestTag, OrgId, ApiKeyPrefix, PrincipalType, SourceIp, RequestTime,
// RequestRegion.
type PolicyStatement struct {
	SID       string                            `json:"sid,omitempty"`
	Effect    string                            `json:"effect"`
	Actions   []string                          `json:"actions"`
	Resources []string                          `json:"resources"`
	Condition map[string]map[string]interface{} `json:"condition,omitempty"`
}

// PolicyDocument matches schemas/iam.py::PolicyDocument.
type PolicyDocument struct {
	Version    string            `json:"version"`
	Statements []PolicyStatement `json:"statements"`
}

// ─── Role ───────────────────────────────────────────────────────────────────

// Role is the response shape for /v1/iam/roles{,/<id>}.
type Role struct {
	ID             string          `json:"id"`
	TenantID       *string         `json:"tenant_id,omitempty"` // null for built-ins
	OrgID          *string         `json:"org_id,omitempty"`
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	PolicyDocument json.RawMessage `json:"policy_document"`
	PolicyHash     string          `json:"policy_hash"`
	IsBuiltIn      bool             `json:"is_built_in"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// RoleCreateRequest is the body of POST /v1/iam/roles. policy_document is
// sent as raw JSON to preserve the canonical layout chosen by the caller
// (the API will re-canonicalize via JCS for the hash).
type RoleCreateRequest struct {
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	PolicyDocument json.RawMessage `json:"policy_document"`
}

// RoleUpdateRequest is the body of PATCH /v1/iam/roles/{id}. All fields
// optional. The API refuses if the target is built-in.
type RoleUpdateRequest struct {
	Name           *string         `json:"name,omitempty"`
	Description    *string         `json:"description,omitempty"`
	PolicyDocument json.RawMessage `json:"policy_document,omitempty"`
}

// ─── Role assignment ────────────────────────────────────────────────────────

// RoleAssignment is the response shape for /v1/iam/roles/{id}/assignments.
type RoleAssignment struct {
	ID            string     `json:"id"`
	RoleID        string     `json:"role_id"`
	TenantID      string     `json:"tenant_id"`
	OrgID         string     `json:"org_id"`
	PrincipalType string     `json:"principal_type"`
	PrincipalID   string     `json:"principal_id"`
	GrantedBy     *string    `json:"granted_by,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// RoleAssignmentCreateRequest is the body of POST /v1/iam/roles/{id}/assignments.
type RoleAssignmentCreateRequest struct {
	PrincipalType string     `json:"principal_type"`
	PrincipalID   string     `json:"principal_id"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// ─── Service account ────────────────────────────────────────────────────────

// ServiceAccount is the response shape for /v1/service-accounts (no token).
type ServiceAccount struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	OrgID       string     `json:"org_id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	TokenPrefix string     `json:"token_prefix"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RotatedAt   *time.Time `json:"rotated_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ServiceAccountWithToken is returned by POST + /rotate (token in clear).
// `token` is one-shot: subsequent reads never re-emit it.
type ServiceAccountWithToken struct {
	ServiceAccount
	Token string `json:"token"`
}

// ServiceAccountCreateRequest is the body of POST /v1/service-accounts.
type ServiceAccountCreateRequest struct {
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	ExpiresInDays *int    `json:"expires_in_days,omitempty"`
}

// ServiceAccountUpdateRequest is the body of PATCH /v1/service-accounts/{id}.
type ServiceAccountUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	PrincipalTypeOrgMember      = "org_member"
	PrincipalTypeApiKey         = "api_key"
	PrincipalTypeServiceAccount = "service_account"
	PrincipalTypeCcksWorkload   = "ccks_workload"

	PolicyVersion20260510 = "2026-05-10"
)
