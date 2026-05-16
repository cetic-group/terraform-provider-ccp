// Application Gateway v1 (ccp-appgw) types — L7 routing with TLS termination,
// rate limiting, IP allow/deny, CORS, basic auth and WAF presets.
//
// Mirrors the API schemas in `apps/api/app/schemas/appgw.py`. The Terraform
// provider exposes 5 resources (gateway / listener / target_group / target_
// group_member / route) + 1 datasource (gateway lookup).
package client

// ─── Application Gateway ─────────────────────────────────────────────────────

type ApplicationGateway struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Region                string   `json:"region"`
	Plan                  string   `json:"plan"`
	VpcID                 string   `json:"vpc_id"`
	VnetID                string   `json:"vnet_id"`
	PublicIPID            *string  `json:"public_ip_id,omitempty"`
	PublicIPAddress       *string  `json:"public_ip_address,omitempty"`
	PublicIPStatus        *string  `json:"public_ip_status,omitempty"`
	VIPAddress            *string  `json:"vip_address,omitempty"`
	Status                string   `json:"status"`
	ErrorMessage          *string  `json:"error_message,omitempty"`
	ForceHTTPS            bool     `json:"force_https"`
	HSTSEnabled           bool     `json:"hsts_enabled"`
	HSTSMaxAge            int64    `json:"hsts_max_age"`
	GlobalRateLimitPerSec *int64   `json:"global_rate_limit_per_sec,omitempty"`
	GlobalAllowCIDRs      []string `json:"global_allow_cidrs"`
	GlobalDenyCIDRs       []string `json:"global_deny_cidrs"`
	TrustProxyHeaders     bool     `json:"trust_proxy_headers"`
	Tags                  []string `json:"tags"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`

	// Optionally embedded by the API on GET (nested lists for datasource use).
	Listeners     []AppGWListener     `json:"listeners,omitempty"`
	TargetGroups  []AppGWTargetGroup  `json:"target_groups,omitempty"`
	Routes        []AppGWRoute        `json:"routes,omitempty"`
}

type ApplicationGatewayCreateRequest struct {
	Name                  string   `json:"name"`
	Region                string   `json:"region"`
	Plan                  string   `json:"plan"`
	VpcID                 string   `json:"vpc_id"`
	VnetID                string   `json:"vnet_id"`
	PublicIPID            *string  `json:"public_ip_id,omitempty"`
	ForceHTTPS            *bool    `json:"force_https,omitempty"`
	HSTSEnabled           *bool    `json:"hsts_enabled,omitempty"`
	HSTSMaxAge            *int64   `json:"hsts_max_age,omitempty"`
	GlobalRateLimitPerSec *int64   `json:"global_rate_limit_per_sec,omitempty"`
	GlobalAllowCIDRs      []string `json:"global_allow_cidrs,omitempty"`
	GlobalDenyCIDRs       []string `json:"global_deny_cidrs,omitempty"`
	TrustProxyHeaders     *bool    `json:"trust_proxy_headers,omitempty"`
	Tags                  []string `json:"tags,omitempty"`
}

type ApplicationGatewayUpdateRequest struct {
	Name                  *string   `json:"name,omitempty"`
	ForceHTTPS            *bool     `json:"force_https,omitempty"`
	HSTSEnabled           *bool     `json:"hsts_enabled,omitempty"`
	HSTSMaxAge            *int64    `json:"hsts_max_age,omitempty"`
	GlobalRateLimitPerSec *int64    `json:"global_rate_limit_per_sec,omitempty"`
	GlobalAllowCIDRs      *[]string `json:"global_allow_cidrs,omitempty"`
	GlobalDenyCIDRs       *[]string `json:"global_deny_cidrs,omitempty"`
	TrustProxyHeaders     *bool     `json:"trust_proxy_headers,omitempty"`
	Tags                  *[]string `json:"tags,omitempty"`
}

type ApplicationGatewayAttachIPRequest struct {
	PublicIPID string `json:"public_ip_id"`
}

// AppGw status enum — kept in sync with `AppGwStatus` on the backend.
// Valid values: creating | active | error | deleting (NO "updating" —
// removed server-side in v1.8.x after pipeline rework).
const (
	AppGWStatusCreating = "creating"
	AppGWStatusActive   = "active"
	AppGWStatusError    = "error"
	AppGWStatusDeleting = "deleting"
)

// Public IP attachment lifecycle exposed via `public_ip_status` on the
// gateway response (mirrors the AppGw public_ip pattern documented in
// `apps/api/CLAUDE.md`). Empty when no IP is attached.
const (
	AppGWPublicIPStatusAllocated = "allocated"
	AppGWPublicIPStatusAttaching = "attaching"
	AppGWPublicIPStatusAttached  = "attached"
	AppGWPublicIPStatusDetaching = "detaching"
	AppGWPublicIPStatusError     = "error"
)

// ─── Listener ───────────────────────────────────────────────────────────────

type AppGWListener struct {
	ID                 string  `json:"id"`
	AppGWID            string  `json:"appgw_id"`
	Hostname           string  `json:"hostname"`
	CustomDomain       bool    `json:"custom_domain"`
	AcmeStatus         string  `json:"acme_status"`
	AcmeLastRenewalAt  *string `json:"acme_last_renewal_at,omitempty"`
	CertPath           *string `json:"cert_path,omitempty"`
	CreatedAt          string  `json:"created_at"`
}

type AppGWListenerCreateRequest struct {
	Hostname     string `json:"hostname"`
	CustomDomain *bool  `json:"custom_domain,omitempty"`
}

// ─── Target Group ───────────────────────────────────────────────────────────

type AppGWTargetGroup struct {
	ID                    string `json:"id"`
	AppGWID               string `json:"appgw_id"`
	Name                  string `json:"name"`
	Algorithm             string `json:"algorithm"`
	HCProtocol            string `json:"hc_protocol"`
	HCMethod              string `json:"hc_method"`
	HCPath                string `json:"hc_path"`
	HCExpectStatus        int64  `json:"hc_expect_status"`
	HCIntervalSec         int64  `json:"hc_interval_sec"`
	HCTimeoutSec          int64  `json:"hc_timeout_sec"`
	HCHealthyThreshold    int64  `json:"hc_healthy_threshold"`
	HCUnhealthyThreshold  int64  `json:"hc_unhealthy_threshold"`
	StickyEnabled         bool   `json:"sticky_enabled"`
	StickyCookieName      *string `json:"sticky_cookie_name,omitempty"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`

	Members []AppGWTargetGroupMember `json:"members,omitempty"`
}

type AppGWTargetGroupCreateRequest struct {
	Name                 string  `json:"name"`
	Algorithm            *string `json:"algorithm,omitempty"`
	HCProtocol           *string `json:"hc_protocol,omitempty"`
	HCMethod             *string `json:"hc_method,omitempty"`
	HCPath               *string `json:"hc_path,omitempty"`
	HCExpectStatus       *int64  `json:"hc_expect_status,omitempty"`
	HCIntervalSec        *int64  `json:"hc_interval_sec,omitempty"`
	HCTimeoutSec         *int64  `json:"hc_timeout_sec,omitempty"`
	HCHealthyThreshold   *int64  `json:"hc_healthy_threshold,omitempty"`
	HCUnhealthyThreshold *int64  `json:"hc_unhealthy_threshold,omitempty"`
	StickyEnabled        *bool   `json:"sticky_enabled,omitempty"`
	StickyCookieName     *string `json:"sticky_cookie_name,omitempty"`
}

type AppGWTargetGroupUpdateRequest struct {
	Name                 *string `json:"name,omitempty"`
	Algorithm            *string `json:"algorithm,omitempty"`
	HCProtocol           *string `json:"hc_protocol,omitempty"`
	HCMethod             *string `json:"hc_method,omitempty"`
	HCPath               *string `json:"hc_path,omitempty"`
	HCExpectStatus       *int64  `json:"hc_expect_status,omitempty"`
	HCIntervalSec        *int64  `json:"hc_interval_sec,omitempty"`
	HCTimeoutSec         *int64  `json:"hc_timeout_sec,omitempty"`
	HCHealthyThreshold   *int64  `json:"hc_healthy_threshold,omitempty"`
	HCUnhealthyThreshold *int64  `json:"hc_unhealthy_threshold,omitempty"`
	StickyEnabled        *bool   `json:"sticky_enabled,omitempty"`
	StickyCookieName     *string `json:"sticky_cookie_name,omitempty"`
}

// ─── Target Group Member ────────────────────────────────────────────────────

type AppGWTargetGroupMember struct {
	ID            string  `json:"id"`
	TargetGroupID string  `json:"target_group_id"`
	ContainerID   *string `json:"container_id,omitempty"`
	VMInstanceID  *string `json:"vm_instance_id,omitempty"`
	TargetIP      *string `json:"target_ip,omitempty"`
	Port          int64   `json:"port"`
	Weight        int64   `json:"weight"`
	Enabled       bool    `json:"enabled"`
	CreatedAt     string  `json:"created_at"`
}

type AppGWTargetGroupMemberCreateRequest struct {
	ContainerID  *string `json:"container_id,omitempty"`
	VMInstanceID *string `json:"vm_instance_id,omitempty"`
	TargetIP     *string `json:"target_ip,omitempty"`
	Port         int64   `json:"port"`
	Weight       *int64  `json:"weight,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

type AppGWTargetGroupMemberUpdateRequest struct {
	Weight  *int64 `json:"weight,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// ─── Route ──────────────────────────────────────────────────────────────────

// AppGWHeaderMatch encodes a per-header condition in a route. `op` is
// expected to be one of `eq` / `prefix` / `regex` (validated server-side).
type AppGWHeaderMatch struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Op    string `json:"op,omitempty"`
}

// AppGWBasicAuthUser is the plaintext credential pair persisted to the
// Secret Manager when the route enables basic auth. The API receives
// plaintext and hashes them server-side; we never get them back on Read.
//
// Wire format aligns with the backend Pydantic schema
// (`AppgwBasicAuthUser`): keys are `user` + `password`. The previous
// `username` JSON tag was silently dropped by Pydantic (extra=ignore)
// and the missing required `user` field returned 422.
type AppGWBasicAuthUser struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type AppGWRoute struct {
	ID                 string             `json:"id"`
	AppGWID            string             `json:"appgw_id"`
	ListenerID         string             `json:"listener_id"`
	Priority           int64              `json:"priority"`
	PathMatch          *string            `json:"path_match,omitempty"`
	PathMatchType      string             `json:"path_match_type"`
	HeaderMatches      []AppGWHeaderMatch `json:"header_matches"`
	MethodMatch        []string           `json:"method_match"`
	TargetGroupID      string             `json:"target_group_id"`
	RateLimitPerSec    *int64             `json:"rate_limit_per_sec,omitempty"`
	AllowCIDRs         []string           `json:"allow_cidrs"`
	DenyCIDRs          []string           `json:"deny_cidrs"`
	RequestHeaders     map[string]string  `json:"request_headers"`
	ResponseHeaders    map[string]string  `json:"response_headers"`
	CORSEnabled        bool               `json:"cors_enabled"`
	CORSOrigins        []string           `json:"cors_origins"`
	CORSMethods        []string           `json:"cors_methods"`
	CORSCredentials    bool               `json:"cors_credentials"`
	BasicAuthSecretRef *string            `json:"basic_auth_secret_ref,omitempty"`
	WAFPreset          string             `json:"waf_preset"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
}

type AppGWRouteCreateRequest struct {
	ListenerID      string               `json:"listener_id"`
	Priority        *int64               `json:"priority,omitempty"`
	PathMatch       *string              `json:"path_match,omitempty"`
	PathMatchType   *string              `json:"path_match_type,omitempty"`
	HeaderMatches   []AppGWHeaderMatch   `json:"header_matches,omitempty"`
	MethodMatch     []string             `json:"method_match,omitempty"`
	TargetGroupID   string               `json:"target_group_id"`
	RateLimitPerSec *int64               `json:"rate_limit_per_sec,omitempty"`
	AllowCIDRs      []string             `json:"allow_cidrs,omitempty"`
	DenyCIDRs       []string             `json:"deny_cidrs,omitempty"`
	RequestHeaders  map[string]string    `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string    `json:"response_headers,omitempty"`
	CORSEnabled     *bool                `json:"cors_enabled,omitempty"`
	CORSOrigins     []string             `json:"cors_origins,omitempty"`
	CORSMethods     []string             `json:"cors_methods,omitempty"`
	CORSCredentials *bool                `json:"cors_credentials,omitempty"`
	BasicAuthUsers  []AppGWBasicAuthUser `json:"basic_auth_users,omitempty"`
	WAFPreset       *string              `json:"waf_preset,omitempty"`
}

type AppGWRouteUpdateRequest struct {
	Priority        *int64                `json:"priority,omitempty"`
	PathMatch       *string               `json:"path_match,omitempty"`
	PathMatchType   *string               `json:"path_match_type,omitempty"`
	HeaderMatches   *[]AppGWHeaderMatch   `json:"header_matches,omitempty"`
	MethodMatch     *[]string             `json:"method_match,omitempty"`
	TargetGroupID   *string               `json:"target_group_id,omitempty"`
	RateLimitPerSec *int64                `json:"rate_limit_per_sec,omitempty"`
	AllowCIDRs      *[]string             `json:"allow_cidrs,omitempty"`
	DenyCIDRs       *[]string             `json:"deny_cidrs,omitempty"`
	RequestHeaders  *map[string]string    `json:"request_headers,omitempty"`
	ResponseHeaders *map[string]string    `json:"response_headers,omitempty"`
	CORSEnabled     *bool                 `json:"cors_enabled,omitempty"`
	CORSOrigins     *[]string             `json:"cors_origins,omitempty"`
	CORSMethods     *[]string             `json:"cors_methods,omitempty"`
	CORSCredentials *bool                 `json:"cors_credentials,omitempty"`
	BasicAuthUsers  *[]AppGWBasicAuthUser `json:"basic_auth_users,omitempty"`
	WAFPreset       *string               `json:"waf_preset,omitempty"`
}
