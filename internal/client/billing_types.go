// Types for the Billing v2 API surface (pricing, free-tier, budgets,
// commits, promo codes, estimate).
//
// Source of truth : cetic-cloud-platform/apps/api/app/api/v1/billing_v2.py
// and apps/api/app/api/v1/billing_credits.py.
package client

import "time"

// ─── Pricing (read-only) ─────────────────────────────────────────────────

type Pricing struct {
	ID                              string  `json:"id"`
	ResourceType                    string  `json:"resource_type"`
	Plan                            *string `json:"plan,omitempty"`
	HourlyPriceCents                int     `json:"hourly_price_cents"`
	MonthlyPriceEUR                 float64 `json:"monthly_price_eur"`
	YearlyPriceEUR                  float64 `json:"yearly_price_eur"`
	Currency                        string  `json:"currency"`
	Description                     *string `json:"description,omitempty"`
	IsFree                          bool    `json:"is_free"`
	BillingDimension                string  `json:"billing_dimension"`
	StoppedDiskPriceCentsPerGBHour  *int    `json:"stopped_disk_price_cents_per_gb_hour,omitempty"`
	MonthlyCommitDiscountPct        int     `json:"monthly_commit_discount_pct"`
	YearlyCommitDiscountPct         int     `json:"yearly_commit_discount_pct"`
}

// ─── Tenant Budgets (CRUD) ────────────────────────────────────────────────

type Budget struct {
	ID                       string     `json:"id"`
	TenantID                 string     `json:"tenant_id"`
	MonthlyBudgetCents       int        `json:"monthly_budget_cents"`
	Currency                 string     `json:"currency"`
	AlertThresholdsPct       []int      `json:"alert_thresholds_pct"`
	NotifyEmails             []string   `json:"notify_emails"`
	HardStopAt100            bool       `json:"hard_stop_at_100"`
	LastAlertThresholdPct    *int       `json:"last_alert_threshold_pct,omitempty"`
	LastAlertAt              *time.Time `json:"last_alert_at,omitempty"`
	Active                   bool       `json:"active"`
}

type BudgetCreateRequest struct {
	MonthlyBudgetCents int      `json:"monthly_budget_cents"`
	AlertThresholdsPct []int    `json:"alert_thresholds_pct,omitempty"`
	NotifyEmails       []string `json:"notify_emails,omitempty"`
	HardStopAt100      bool     `json:"hard_stop_at_100"`
}

// ─── Tenant Commits (engagement) ──────────────────────────────────────────

type Commit struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	CommitType  string     `json:"commit_type"`
	DiscountPct int        `json:"discount_pct"`
	StartAt     time.Time  `json:"start_at"`
	EndAt       time.Time  `json:"end_at"`
	AutoRenew   bool       `json:"auto_renew"`
	CanceledAt  *time.Time `json:"canceled_at,omitempty"`
}

type CommitCreateRequest struct {
	CommitType string `json:"commit_type"`
	AutoRenew  bool   `json:"auto_renew"`
}

// ─── Promo codes (public list + apply) ────────────────────────────────────

type PromoCode struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	Description     string     `json:"description"`
	DiscountPct     int        `json:"discount_pct"`
	DurationMonths  int        `json:"duration_months"`
	MaxRedemptions  *int       `json:"max_redemptions,omitempty"`
	RedeemedCount   int        `json:"redeemed_count"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidUntil      *time.Time `json:"valid_until,omitempty"`
	Active          bool       `json:"active"`
}
