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

// ResizeRegistryDisk grows the registry's storage quota. Grow-only — the API
// rejects a storageGB smaller than the current quota (#577).
func (c *Client) ResizeRegistryDisk(ctx context.Context, id string, storageGB int) (*Registry, error) {
	var out Registry
	if err := c.do(ctx, http.MethodPost, "/v1/registries/"+id+"/resize-disk",
		RegistryResizeDiskRequest{StorageGB: storageGB}, &out); err != nil {
		return nil, err
	}
	return &out, nil
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

// DeleteRegistryUser deletes a registry user by its UUID: the API route is
// `/users/{user_id}` (the username in the path got a 422 and the user was
// never deleted, cetic-cloud-platform#2004).
func (c *Client) DeleteRegistryUser(ctx context.Context, registryID, userID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/registries/"+registryID+"/users/"+userID, nil, nil)
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

// ─── Registry cleanup schedules (cetic-cloud-platform#1984) ─────────────────

// ListRegistryCleanupSchedules returns the schedules of a registry, ordered by
// slot. The API has no GET for a single schedule: readers look it up here.
func (c *Client) ListRegistryCleanupSchedules(ctx context.Context, registryID string) ([]RegistryCleanupSchedule, error) {
	var out []RegistryCleanupSchedule
	if err := c.do(ctx, http.MethodGet, "/v1/registries/"+registryID+"/cleanup/schedules", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateRegistryCleanupSchedule(ctx context.Context, registryID string, req RegistryCleanupScheduleWrite) (*RegistryCleanupSchedule, error) {
	var out RegistryCleanupSchedule
	if err := c.do(ctx, http.MethodPost, "/v1/registries/"+registryID+"/cleanup/schedules", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateRegistryCleanupSchedule(ctx context.Context, registryID, scheduleID string, req RegistryCleanupScheduleWrite) (*RegistryCleanupSchedule, error) {
	var out RegistryCleanupSchedule
	if err := c.do(ctx, http.MethodPatch, "/v1/registries/"+registryID+"/cleanup/schedules/"+scheduleID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteRegistryCleanupSchedule(ctx context.Context, registryID, scheduleID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/registries/"+registryID+"/cleanup/schedules/"+scheduleID, nil, nil)
}
