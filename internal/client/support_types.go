package client

import "time"

// SupportPlan — catalogue support plan (vague C6).
type SupportPlan struct {
	ID                    string                 `json:"id"`
	Key                   string                 `json:"key"`
	DisplayName           string                 `json:"display_name"`
	Description           string                 `json:"description"`
	PriceEurMonthCents    int                    `json:"price_eur_month_cents"`
	SlaFirstResponseHours int                    `json:"sla_first_response_hours"`
	SlaResolutionHours    *int                   `json:"sla_resolution_hours,omitempty"`
	MaxPriority           string                 `json:"max_priority"`
	Channels              []string               `json:"channels"`
	IsDefault             bool                   `json:"is_default"`
	IsActive              bool                   `json:"is_active"`
	SortOrder             int                    `json:"sort_order"`
	Features              map[string]interface{} `json:"features"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
}

// SupportSubscription — historique d'une bascule de plan.
type SupportSubscription struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	PlanKey   string     `json:"plan_key"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Reason    *string    `json:"reason,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// CurrentSupportPlan — réponse de GET /v1/support/subscription.
type CurrentSupportPlan struct {
	Subscription *SupportSubscription `json:"subscription,omitempty"`
	Plan         *SupportPlan         `json:"plan,omitempty"`
}
