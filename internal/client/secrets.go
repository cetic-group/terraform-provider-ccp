// Client methods for the /v1/secrets/* API surface (Secret Manager v1).
//
// Kept in a dedicated file because client.go already exceeds 1500 lines.
//
// SECURITY — the `data` field (raw secret values) is intentionally NOT
// exposed by the typed `Secret` shape: GET endpoints never return it. Only
// the dedicated reveal endpoint `/v1/secrets/{id}/value` returns it, and
// the provider does NOT call it from CRUD (drift detection on `data` is
// deliberately disabled to avoid hammering the audit log).
package client

import (
	"context"
	"net/http"
)

// ─── Secret ────────────────────────────────────────────────────────────────

// Secret is the response shape for /v1/secrets{,/<id>}.
// `data` is intentionally absent — the reveal endpoint is separate and
// audit-logged.
type Secret struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   *string  `json:"description,omitempty"`
	Version       int64    `json:"version"`
	Tags          []string `json:"tags"`
	LastRotatedAt *string  `json:"last_rotated_at,omitempty"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// SecretCreatePayload is the body of POST /v1/secrets.
type SecretCreatePayload struct {
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	Data        map[string]string `json:"data"`
	Tags        []string          `json:"tags"`
}

// SecretUpdatePayload is the body of PATCH /v1/secrets/{id}.
// All fields optional — pass nil to leave unchanged.
type SecretUpdatePayload struct {
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

// SecretRotatePayload is the body of POST /v1/secrets/{id}/rotate.
type SecretRotatePayload struct {
	Data map[string]string `json:"data"`
}

// CreateSecret creates a secret with its initial encrypted payload.
// The response does NOT echo `data` back — callers must keep the plan
// values to persist into state.
func (c *Client) CreateSecret(ctx context.Context, p SecretCreatePayload) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodPost, "/v1/secrets", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSecret fetches metadata for a single secret by UUID. 404 if not
// visible. Never returns `data`.
func (c *Client) GetSecret(ctx context.Context, id string) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodGet, "/v1/secrets/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSecrets returns metadata for every secret visible to the current
// org. Never returns `data`.
func (c *Client) ListSecrets(ctx context.Context) ([]Secret, error) {
	var out []Secret
	if err := c.do(ctx, http.MethodGet, "/v1/secrets", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetSecretByName looks up a secret by name within the current org.
// Implemented client-side as ListSecrets + filter (the v1 API does not
// support `?name=` query). Returns 404 if not found.
func (c *Client) GetSecretByName(ctx context.Context, name string) (*Secret, error) {
	list, err := c.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Method:     http.MethodGet,
		Path:       "/v1/secrets?name=" + name,
		Detail:     "secret not found",
	}
}

// UpdateSecret patches mutable metadata (description / tags). Pass
// nil fields to leave them unchanged.
func (c *Client) UpdateSecret(ctx context.Context, id string, p SecretUpdatePayload) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodPatch, "/v1/secrets/"+id, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RotateSecret overwrites the encrypted payload with new `data` and bumps
// `version` + `last_rotated_at` server-side. Audit-logged.
func (c *Client) RotateSecret(ctx context.Context, id string, p SecretRotatePayload) (*Secret, error) {
	var out Secret
	if err := c.do(ctx, http.MethodPost, "/v1/secrets/"+id+"/rotate", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSecret removes a secret. 404 is up to the caller to handle (use
// `IsNotFound`) for idempotent destroys.
func (c *Client) DeleteSecret(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/secrets/"+id, nil, nil)
}
