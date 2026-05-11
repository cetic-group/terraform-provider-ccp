// Client methods for the /v1/iam/* API surface (Roles v1).
//
// Kept in a dedicated file because client.go already exceeds 1500 lines.
package client

import (
	"context"
	"net/http"
	"net/url"
)

// ─── Roles ──────────────────────────────────────────────────────────────────

// ListRoles returns custom + built-in roles visible to the current tenant.
// If `builtIn != nil`, filters server-side: true → built-ins only,
// false → custom only.
func (c *Client) ListRoles(ctx context.Context, builtIn *bool) ([]Role, error) {
	path := "/v1/iam/roles"
	if builtIn != nil {
		if *builtIn {
			path += "?built_in=true"
		} else {
			path += "?built_in=false"
		}
	}
	var out []Role
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBuiltInRoles fetches the catalog of the 10 built-in roles (seeded
// by migration 140). Equivalent to ListRoles(ctx, &true) but uses the
// dedicated read-only endpoint for clarity.
func (c *Client) ListBuiltInRoles(ctx context.Context) ([]Role, error) {
	var out []Role
	if err := c.do(ctx, http.MethodGet, "/v1/iam/built-in-roles", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRole fetches a single role by ID. 404 if not visible to caller.
func (c *Client) GetRole(ctx context.Context, id string) (*Role, error) {
	var out Role
	if err := c.do(ctx, http.MethodGet, "/v1/iam/roles/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRoleByName looks up a role by name. Returns 404 if not found.
// Used by the ccp_iam_role data source.
func (c *Client) GetRoleByName(ctx context.Context, name string, builtIn *bool) (*Role, error) {
	roles, err := c.ListRoles(ctx, builtIn)
	if err != nil {
		return nil, err
	}
	for i := range roles {
		if roles[i].Name == name {
			if builtIn == nil || roles[i].IsBuiltIn == *builtIn {
				return &roles[i], nil
			}
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Method:     http.MethodGet,
		Path:       "/v1/iam/roles?name=" + url.QueryEscape(name),
		Detail:     "iam role not found",
	}
}

// CreateRole creates a custom role. Built-ins are seeded server-side only.
func (c *Client) CreateRole(ctx context.Context, req RoleCreateRequest) (*Role, error) {
	var out Role
	if err := c.do(ctx, http.MethodPost, "/v1/iam/roles", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateRole patches a custom role. Refused (403) on built-ins.
func (c *Client) UpdateRole(ctx context.Context, id string, req RoleUpdateRequest) (*Role, error) {
	var out Role
	if err := c.do(ctx, http.MethodPatch, "/v1/iam/roles/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRole removes a custom role. Refused (403) on built-ins, refused
// (409) if assignments still exist.
func (c *Client) DeleteRole(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/iam/roles/"+id, nil, nil)
}

// ─── Role assignments ───────────────────────────────────────────────────────

// ListRoleAssignments fetches all assignments for a role within the
// current tenant.
func (c *Client) ListRoleAssignments(ctx context.Context, roleID string) ([]RoleAssignment, error) {
	var out []RoleAssignment
	if err := c.do(ctx, http.MethodGet, "/v1/iam/roles/"+roleID+"/assignments", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRoleAssignment fetches a single assignment via list+filter (there is
// no GET /roles/{rid}/assignments/{aid} endpoint).
func (c *Client) GetRoleAssignment(ctx context.Context, roleID, assignmentID string) (*RoleAssignment, error) {
	list, err := c.ListRoleAssignments(ctx, roleID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == assignmentID {
			return &list[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Method:     http.MethodGet,
		Path:       "/v1/iam/roles/" + roleID + "/assignments/" + assignmentID,
		Detail:     "iam role assignment not found",
	}
}

// CreateRoleAssignment attaches a role to a principal.
func (c *Client) CreateRoleAssignment(ctx context.Context, roleID string, req RoleAssignmentCreateRequest) (*RoleAssignment, error) {
	var out RoleAssignment
	if err := c.do(ctx, http.MethodPost, "/v1/iam/roles/"+roleID+"/assignments", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRoleAssignment detaches a role from a principal.
func (c *Client) DeleteRoleAssignment(ctx context.Context, roleID, assignmentID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/iam/roles/"+roleID+"/assignments/"+assignmentID, nil, nil)
}

// ─── Service accounts ──────────────────────────────────────────────────────

// ListServiceAccounts returns the SAs in the current org.
func (c *Client) ListServiceAccounts(ctx context.Context) ([]ServiceAccount, error) {
	var out []ServiceAccount
	if err := c.do(ctx, http.MethodGet, "/v1/service-accounts", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetServiceAccount fetches a single SA by ID.
func (c *Client) GetServiceAccount(ctx context.Context, id string) (*ServiceAccount, error) {
	var out ServiceAccount
	if err := c.do(ctx, http.MethodGet, "/v1/service-accounts/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateServiceAccount creates a SA and returns the one-shot token.
// Callers MUST persist the token before any Read(), the API never re-emits it.
func (c *Client) CreateServiceAccount(ctx context.Context, req ServiceAccountCreateRequest) (*ServiceAccountWithToken, error) {
	var out ServiceAccountWithToken
	if err := c.do(ctx, http.MethodPost, "/v1/service-accounts", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateServiceAccount patches name/description.
func (c *Client) UpdateServiceAccount(ctx context.Context, id string, req ServiceAccountUpdateRequest) (*ServiceAccount, error) {
	var out ServiceAccount
	if err := c.do(ctx, http.MethodPatch, "/v1/service-accounts/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RotateServiceAccount rotates the SA token. Old token is invalidated
// immediately (no grace period). Returns new one-shot token.
func (c *Client) RotateServiceAccount(ctx context.Context, id string) (*ServiceAccountWithToken, error) {
	var out ServiceAccountWithToken
	if err := c.do(ctx, http.MethodPost, "/v1/service-accounts/"+id+"/rotate", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteServiceAccount removes a SA.
func (c *Client) DeleteServiceAccount(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/service-accounts/"+id, nil, nil)
}
