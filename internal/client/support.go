// Client methods for the Support Plans API (vague C6).
package client

import (
	"context"
	"net/http"
)

// ─── Support plans (catalogue) ────────────────────────────────────────────

func (c *Client) ListSupportPlans(ctx context.Context) ([]SupportPlan, error) {
	var out []SupportPlan
	if err := c.do(ctx, http.MethodGet, "/v1/support/plans", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetSupportPlan(ctx context.Context, key string) (*SupportPlan, error) {
	var out SupportPlan
	if err := c.do(ctx, http.MethodGet, "/v1/support/plans/"+key, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Tenant subscription ──────────────────────────────────────────────────

func (c *Client) GetCurrentSupportSubscription(ctx context.Context) (*CurrentSupportPlan, error) {
	var out CurrentSupportPlan
	if err := c.do(ctx, http.MethodGet, "/v1/support/subscription", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SubscribeSupportPlan(ctx context.Context, planKey string) (*SupportSubscription, error) {
	body := map[string]string{"plan_key": planKey}
	var out SupportSubscription
	if err := c.do(ctx, http.MethodPost, "/v1/support/subscribe", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UnsubscribeSupportPlan(ctx context.Context) (*SupportSubscription, error) {
	var out SupportSubscription
	if err := c.do(ctx, http.MethodPost, "/v1/support/unsubscribe", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
