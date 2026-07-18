// Client methods for the /v1/schedules/* API surface (start/stop scheduler).
//
// A schedule is a polymorphic on/off planner attached to a single resource
// (VM, container, scale set member set, CCKS node pool or database instance).
// The target is addressed by (resource_type, resource_id) — there is no
// foreign key server-side, so the target type is immutable once created.
//
// The API rejects a schedule that would flap the resource (windows shorter
// than an hour, more than two on/off cycles per day, overlapping windows, …)
// with HTTP 422 and a structured `{"code","message"}` detail. The generic
// error decoder in client.go surfaces the human-readable `message` into
// APIError.Detail so the resource can bubble it straight into a Terraform
// diagnostic.
package client

import (
	"context"
	"net/http"
)

// ─── Schedule ────────────────────────────────────────────────────────────────

// ScheduleWindow is one weekly OFF interval. The resource is powered OFF
// during [start → end) (ISO week, wrap-around allowed when end precedes
// start) and ON outside of it. Hours are whole-hour aligned (0..24).
type ScheduleWindow struct {
	StartDay  int `json:"start_day"`
	StartHour int `json:"start_hour"`
	EndDay    int `json:"end_day"`
	EndHour   int `json:"end_hour"`
}

// Schedule is the response shape for /v1/schedules{,/<id>}.
type Schedule struct {
	ID                       string           `json:"id"`
	Name                     string           `json:"name"`
	ResourceType             string           `json:"resource_type"`
	ResourceID               string           `json:"resource_id"`
	Timezone                 string           `json:"timezone"`
	Enabled                  bool             `json:"enabled"`
	Windows                  []ScheduleWindow `json:"windows"`
	CurrentState             string           `json:"current_state"`
	LastTransitionAt         *string          `json:"last_transition_at,omitempty"`
	EstimatedMonthlyFeeCents int64            `json:"estimated_monthly_fee_cents"`
	CreatedAt                string           `json:"created_at"`
	UpdatedAt                string           `json:"updated_at"`
}

// ScheduleCreatePayload is the body of POST /v1/schedules.
type ScheduleCreatePayload struct {
	Name         string           `json:"name"`
	ResourceType string           `json:"resource_type"`
	ResourceID   string           `json:"resource_id"`
	Timezone     *string          `json:"timezone,omitempty"`
	Enabled      *bool            `json:"enabled,omitempty"`
	Windows      []ScheduleWindow `json:"windows"`
}

// ScheduleUpdatePayload is the body of PATCH /v1/schedules/{id}.
// Only name / timezone / enabled / windows are mutable — resource_type and
// resource_id are immutable server-side. Pass nil to leave a field unchanged.
type ScheduleUpdatePayload struct {
	Name     *string           `json:"name,omitempty"`
	Timezone *string           `json:"timezone,omitempty"`
	Enabled  *bool             `json:"enabled,omitempty"`
	Windows  *[]ScheduleWindow `json:"windows,omitempty"`
}

// CreateSchedule registers a new start/stop schedule. Returns 422 (flapping),
// 429 (quota) or 404 (target outside the org) as *APIError.
func (c *Client) CreateSchedule(ctx context.Context, p ScheduleCreatePayload) (*Schedule, error) {
	var out Schedule
	if err := c.do(ctx, http.MethodPost, "/v1/schedules", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSchedule fetches a single schedule by UUID. 404 if not visible.
func (c *Client) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	var out Schedule
	if err := c.do(ctx, http.MethodGet, "/v1/schedules/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSchedules returns every schedule visible to the current org.
func (c *Client) ListSchedules(ctx context.Context) ([]Schedule, error) {
	var out []Schedule
	if err := c.do(ctx, http.MethodGet, "/v1/schedules", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetScheduleByName looks up a schedule by name within the current org.
// Implemented client-side as ListSchedules + filter (names are unique per
// org). Returns *APIError(404) if not found.
func (c *Client) GetScheduleByName(ctx context.Context, name string) (*Schedule, error) {
	list, err := c.ListSchedules(ctx)
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
		Path:       "/v1/schedules?name=" + name,
		Detail:     "schedule not found",
	}
}

// UpdateSchedule patches mutable fields (name / timezone / enabled / windows).
// Same 422 validation as create when windows change.
func (c *Client) UpdateSchedule(ctx context.Context, id string, p ScheduleUpdatePayload) (*Schedule, error) {
	var out Schedule
	if err := c.do(ctx, http.MethodPatch, "/v1/schedules/"+id, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSchedule removes a schedule and powers the target back ON. 404 is up
// to the caller to handle (use IsNotFound) for idempotent destroys.
func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/schedules/"+id, nil, nil)
}
