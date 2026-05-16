// Client methods for the Billing v2 API surface.
package client

import (
	"context"
	"net/http"
	"net/url"
)

// ─── Pricing ──────────────────────────────────────────────────────────────

func (c *Client) ListPricing(ctx context.Context) ([]Pricing, error) {
	var out []Pricing
	if err := c.do(ctx, http.MethodGet, "/v1/billing/pricing-v2", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ─── Budgets ──────────────────────────────────────────────────────────────

func (c *Client) ListBudgets(ctx context.Context) ([]Budget, error) {
	var out []Budget
	if err := c.do(ctx, http.MethodGet, "/v1/billing/budgets", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetBudget(ctx context.Context, id string) (*Budget, error) {
	// L'API n'expose pas un GET single — on liste + filtre.
	budgets, err := c.ListBudgets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range budgets {
		if budgets[i].ID == id {
			return &budgets[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Detail: "budget not found"}
}

func (c *Client) CreateBudget(ctx context.Context, req BudgetCreateRequest) (*Budget, error) {
	var out Budget
	if err := c.do(ctx, http.MethodPost, "/v1/billing/budgets", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateBudget(ctx context.Context, id string, req BudgetCreateRequest) (*Budget, error) {
	var out Budget
	if err := c.do(ctx, http.MethodPatch, "/v1/billing/budgets/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteBudget(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/billing/budgets/"+id, nil, nil)
}

// ─── Commits ──────────────────────────────────────────────────────────────

func (c *Client) ListCommits(ctx context.Context) ([]Commit, error) {
	var out []Commit
	if err := c.do(ctx, http.MethodGet, "/v1/billing/commits", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetCommit(ctx context.Context, id string) (*Commit, error) {
	commits, err := c.ListCommits(ctx)
	if err != nil {
		return nil, err
	}
	for i := range commits {
		if commits[i].ID == id {
			return &commits[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Detail: "commit not found"}
}

func (c *Client) CreateCommit(ctx context.Context, req CommitCreateRequest) (*Commit, error) {
	var out Commit
	if err := c.do(ctx, http.MethodPost, "/v1/billing/commits", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CancelCommit(ctx context.Context, id string) (*Commit, error) {
	var out Commit
	if err := c.do(ctx, http.MethodPost, "/v1/billing/commits/"+id+"/cancel", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Promo codes ──────────────────────────────────────────────────────────

func (c *Client) ListAvailablePromoCodes(ctx context.Context) ([]PromoCode, error) {
	var out []PromoCode
	if err := c.do(ctx, http.MethodGet, "/v1/billing/promo-codes/available", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ApplyPromoCode(ctx context.Context, code string) (*PromoCode, error) {
	body := map[string]string{"code": code}
	var out PromoCode
	if err := c.do(ctx, http.MethodPost, "/v1/billing/promo-codes/apply", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Suppress unused import warning (kept for symmetry with other clients).
var _ = url.Values{}
