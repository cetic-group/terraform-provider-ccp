// Client methods for the /v1/registries/* API surface (CCR — Phase 6).
//
// Kept in a dedicated file because client.go already weighs ~1500 lines.
package client

import (
	"context"
	"net/http"
)

// ─── Registries (CRUD + IP attach/detach) ─────────────────────────────────────

func (c *Client) ListRegistries(ctx context.Context) ([]Registry, error) {
	var out []Registry
	if err := c.do(ctx, http.MethodGet, "/v1/registries", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetRegistry(ctx context.Context, id string) (*Registry, error) {
	var out Registry
	if err := c.do(ctx, http.MethodGet, "/v1/registries/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateRegistry returns the *full* response including the one-shot admin
// password. Callers MUST persist that password into state before any
// subsequent Read() (which will not re-emit it).
func (c *Client) CreateRegistry(ctx context.Context, req RegistryCreateRequest) (*RegistryCreateResponse, error) {
	var out RegistryCreateResponse
	if err := c.do(ctx, http.MethodPost, "/v1/registries", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateRegistry(ctx context.Context, id string, req RegistryUpdateRequest) (*Registry, error) {
	var out Registry
	if err := c.do(ctx, http.MethodPatch, "/v1/registries/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRegistry(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/registries/"+id, nil, nil)
}

// ─── Registry users ───────────────────────────────────────────────────────────

func (c *Client) ListRegistryUsers(ctx context.Context, registryID string) ([]RegistryUser, error) {
	var out []RegistryUser
	if err := c.do(ctx, http.MethodGet, "/v1/registries/"+registryID+"/users", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateRegistryUser returns the user struct plus the one-shot password —
// same pattern as CreateRegistry (admin_password) and CreateApiKey (token).
func (c *Client) CreateRegistryUser(ctx context.Context, registryID string, req RegistryUserCreateRequest) (*RegistryUserCreateResponse, error) {
	var out RegistryUserCreateResponse
	if err := c.do(ctx, http.MethodPost, "/v1/registries/"+registryID+"/users", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRegistryUser(ctx context.Context, registryID, username string) error {
	return c.do(ctx, http.MethodDelete, "/v1/registries/"+registryID+"/users/"+username, nil, nil)
}

// ─── Registry ACLs ────────────────────────────────────────────────────────────

func (c *Client) ListRegistryACLs(ctx context.Context, registryID string) ([]RegistryACL, error) {
	var out []RegistryACL
	if err := c.do(ctx, http.MethodGet, "/v1/registries/"+registryID+"/acls", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateRegistryACL(ctx context.Context, registryID string, req RegistryACLCreateRequest) (*RegistryACL, error) {
	var out RegistryACL
	if err := c.do(ctx, http.MethodPost, "/v1/registries/"+registryID+"/acls", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateRegistryACL(ctx context.Context, registryID, aclID string, req RegistryACLUpdateRequest) (*RegistryACL, error) {
	var out RegistryACL
	if err := c.do(ctx, http.MethodPatch, "/v1/registries/"+registryID+"/acls/"+aclID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRegistryACL(ctx context.Context, registryID, aclID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/registries/"+registryID+"/acls/"+aclID, nil, nil)
}
