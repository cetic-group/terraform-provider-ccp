// Application Gateway v1 types — L7 routing with TLS termination,
// rate limiting, IP allow/deny, CORS, basic auth and WAF presets.
//
// Mirrors the API schemas. The Terraform
// provider exposes 5 resources (gateway / listener / target_group / target_
// group_member / route) + 1 datasource (gateway lookup).
package client

// ─── Application Gateway ─────────────────────────────────────────────────────

type ApplicationGateway struct {
	ReplicaCount          *int64   `json:"replica_count,omitempty"`
	CacheEnabled          bool     `json:"cache_enabled"`
	CacheSizeMB           int64    `json:"cache_size_mb"`
	CacheMaxObjectKB      int64    `json:"cache_max_object_kb"`
	CacheSizeLimitMB      int64    `json:"cache_size_limit_mb"`
	CacheEffectiveSizeMB  int64    `json:"cache_effective_size_mb"`
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
	// Réseau d'attache résolu côté serveur (#1579).
	VpcName  *string `json:"vpc_name,omitempty"`
	VnetName *string `json:"vnet_name,omitempty"`
	VnetCIDR *string `json:"vnet_cidr,omitempty"`
	// Page d'indisponibilité du client (#1579) — nil = page standard.
	CustomErrorPageHTML *string `json:"custom_error_page_html,omitempty"`

	// Optionally embedded by the API on GET (nested lists for datasource use).
	Listeners    []AppGWListener    `json:"listeners,omitempty"`
	TargetGroups []AppGWTargetGroup `json:"target_groups,omitempty"`
	Routes       []AppGWRoute       `json:"routes,omitempty"`
}

type ApplicationGatewayCreateRequest struct {
	ReplicaCount          *int64   `json:"replica_count,omitempty"`
	CacheEnabled          *bool    `json:"cache_enabled,omitempty"`
	Name                  string   `json:"name"`
	Region                string   `json:"region"`
	Plan                  string   `json:"plan"`
	VpcID                 string   `json:"vpc_id"`
	VnetID                string   `json:"vnet_id"`
	PublicIPID            *string  `json:"public_ip_id,omitempty"`
	VIPAddress            *string  `json:"vip_address,omitempty"`
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
	Plan                  *string   `json:"plan,omitempty"`
	CacheEnabled          *bool     `json:"cache_enabled,omitempty"`
	Name                  *string   `json:"name,omitempty"`
	ForceHTTPS            *bool     `json:"force_https,omitempty"`
	HSTSEnabled           *bool     `json:"hsts_enabled,omitempty"`
	HSTSMaxAge            *int64    `json:"hsts_max_age,omitempty"`
	GlobalRateLimitPerSec *int64    `json:"global_rate_limit_per_sec,omitempty"`
	GlobalAllowCIDRs      *[]string `json:"global_allow_cidrs,omitempty"`
	GlobalDenyCIDRs       *[]string `json:"global_deny_cidrs,omitempty"`
	TrustProxyHeaders     *bool     `json:"trust_proxy_headers,omitempty"`
	Tags                  *[]string `json:"tags,omitempty"`
	// nil = clé absente (inchangée) ; &NullableString{nil} = `null` (page standard).
	CustomErrorPageHTML *NullableString `json:"custom_error_page_html,omitempty"`
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
// gateway response (same pattern as the other public IP consumers). Empty when no IP is attached.
const (
	AppGWPublicIPStatusAllocated = "allocated"
	AppGWPublicIPStatusAttaching = "attaching"
	AppGWPublicIPStatusAttached  = "attached"
	AppGWPublicIPStatusDetaching = "detaching"
	AppGWPublicIPStatusError     = "error"
)

// ─── Listener ───────────────────────────────────────────────────────────────

type AppGWListener struct {
	ID                string  `json:"id"`
	AppGWID           string  `json:"appgw_id"`
	Hostname          string  `json:"hostname"`
	AcmeStatus        string  `json:"acme_status"`
	AcmeChallenge     *string `json:"acme_challenge,omitempty"`
	AcmeDNSProvider   *string `json:"acme_dns_provider,omitempty"`
	AcmeLastRenewalAt *string `json:"acme_last_renewal_at,omitempty"`
	AcmeIssuedAt      *string `json:"acme_issued_at,omitempty"`
	AcmeRenewAfter    *string `json:"acme_renew_after,omitempty"`
	AcmeLastError     *string `json:"acme_last_error,omitempty"`
	CertPath          *string `json:"cert_path,omitempty"`
	// Certificat fourni par le client (#1579) — métadonnées seulement.
	TLSCertNotAfter *string `json:"tls_cert_not_after,omitempty"`
	TLSCertNames    *string `json:"tls_cert_names,omitempty"`
	TLSCertIssuer   *string `json:"tls_cert_issuer,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type AppGWListenerCreateRequest struct {
	Hostname           string            `json:"hostname"`
	AcmeChallenge      *string           `json:"acme_challenge,omitempty"`
	AcmeDNSProvider    *string           `json:"acme_dns_provider,omitempty"`
	AcmeDNSCredentials map[string]string `json:"acme_dns_credentials,omitempty"`
	// Avec acme_challenge = "custom" (#1579).
	TLSCertificatePEM *string `json:"tls_certificate_pem,omitempty"`
	TLSPrivateKeyPEM  *string `json:"tls_private_key_pem,omitempty"`
}

// ─── Target Group ───────────────────────────────────────────────────────────

type AppGWTargetGroup struct {
	ID                   string  `json:"id"`
	AppGWID              string  `json:"appgw_id"`
	Name                 string  `json:"name"`
	Algorithm            string  `json:"algorithm"`
	HCProtocol           string  `json:"hc_protocol"`
	HCMethod             string  `json:"hc_method"`
	HCPath               string  `json:"hc_path"`
	HCExpectStatus       int64   `json:"hc_expect_status"`
	HCIntervalSec        int64   `json:"hc_interval_sec"`
	HCTimeoutSec         int64   `json:"hc_timeout_sec"`
	HCHealthyThreshold   int64   `json:"hc_healthy_threshold"`
	HCUnhealthyThreshold int64   `json:"hc_unhealthy_threshold"`
	StickyEnabled        bool    `json:"sticky_enabled"`
	StickyCookieName     *string `json:"sticky_cookie_name,omitempty"`
	CreatedAt            string  `json:"created_at"`
	UpdatedAt            string  `json:"updated_at"`

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
	// Cibles dynamiques (#1579).
	ContainerScaleSetID *string `json:"container_scale_set_id,omitempty"`
	VMScaleSetID        *string `json:"vm_scale_set_id,omitempty"`
	Port                int64   `json:"port"`
	Weight              int64   `json:"weight"`
	Enabled             bool    `json:"enabled"`
	CreatedAt           string  `json:"created_at"`
}

type AppGWTargetGroupMemberCreateRequest struct {
	ContainerID  *string `json:"container_id,omitempty"`
	VMInstanceID *string `json:"vm_instance_id,omitempty"`
	TargetIP     *string `json:"target_ip,omitempty"`
	// Cibles dynamiques (#1579).
	ContainerScaleSetID *string `json:"container_scale_set_id,omitempty"`
	VMScaleSetID        *string `json:"vm_scale_set_id,omitempty"`
	Port                int64   `json:"port"`
	Weight              *int64  `json:"weight,omitempty"`
	Enabled             *bool   `json:"enabled,omitempty"`
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
	CacheMode          string             `json:"cache_mode"`
	CacheDefaultTTLS   int64              `json:"cache_default_ttl_s"`
	CacheMaxTTLS       int64              `json:"cache_max_ttl_s"`
	CacheBypassCookies []string           `json:"cache_bypass_cookies"`
	CacheDebugHeaders  bool               `json:"cache_debug_headers"`
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
	StripPrefix        bool               `json:"strip_prefix"`
	CreatedAt          string             `json:"created_at"`
	UpdatedAt          string             `json:"updated_at"`
}

type AppGWRouteCreateRequest struct {
	CacheMode          *string              `json:"cache_mode,omitempty"`
	CacheDefaultTTLS   *int64               `json:"cache_default_ttl_s,omitempty"`
	CacheMaxTTLS       *int64               `json:"cache_max_ttl_s,omitempty"`
	CacheBypassCookies *[]string            `json:"cache_bypass_cookies,omitempty"`
	CacheDebugHeaders  *bool                `json:"cache_debug_headers,omitempty"`
	ListenerID         string               `json:"listener_id"`
	Priority           *int64               `json:"priority,omitempty"`
	PathMatch          *string              `json:"path_match,omitempty"`
	PathMatchType      *string              `json:"path_match_type,omitempty"`
	HeaderMatches      []AppGWHeaderMatch   `json:"header_matches,omitempty"`
	MethodMatch        []string             `json:"method_match,omitempty"`
	TargetGroupID      string               `json:"target_group_id"`
	RateLimitPerSec    *int64               `json:"rate_limit_per_sec,omitempty"`
	AllowCIDRs         []string             `json:"allow_cidrs,omitempty"`
	DenyCIDRs          []string             `json:"deny_cidrs,omitempty"`
	RequestHeaders     map[string]string    `json:"request_headers,omitempty"`
	ResponseHeaders    map[string]string    `json:"response_headers,omitempty"`
	CORSEnabled        *bool                `json:"cors_enabled,omitempty"`
	CORSOrigins        []string             `json:"cors_origins,omitempty"`
	CORSMethods        []string             `json:"cors_methods,omitempty"`
	CORSCredentials    *bool                `json:"cors_credentials,omitempty"`
	BasicAuthUsers     []AppGWBasicAuthUser `json:"basic_auth_users,omitempty"`
	WAFPreset          *string              `json:"waf_preset,omitempty"`
	StripPrefix        *bool                `json:"strip_prefix,omitempty"`
}

type AppGWRouteUpdateRequest struct {
	CacheMode          *string               `json:"cache_mode,omitempty"`
	CacheDefaultTTLS   *int64                `json:"cache_default_ttl_s,omitempty"`
	CacheMaxTTLS       *int64                `json:"cache_max_ttl_s,omitempty"`
	CacheBypassCookies *[]string             `json:"cache_bypass_cookies,omitempty"`
	CacheDebugHeaders  *bool                 `json:"cache_debug_headers,omitempty"`
	Priority           *int64                `json:"priority,omitempty"`
	PathMatch          *string               `json:"path_match,omitempty"`
	PathMatchType      *string               `json:"path_match_type,omitempty"`
	HeaderMatches      *[]AppGWHeaderMatch   `json:"header_matches,omitempty"`
	MethodMatch        *[]string             `json:"method_match,omitempty"`
	TargetGroupID      *string               `json:"target_group_id,omitempty"`
	RateLimitPerSec    *int64                `json:"rate_limit_per_sec,omitempty"`
	AllowCIDRs         *[]string             `json:"allow_cidrs,omitempty"`
	DenyCIDRs          *[]string             `json:"deny_cidrs,omitempty"`
	RequestHeaders     *map[string]string    `json:"request_headers,omitempty"`
	ResponseHeaders    *map[string]string    `json:"response_headers,omitempty"`
	CORSEnabled        *bool                 `json:"cors_enabled,omitempty"`
	CORSOrigins        *[]string             `json:"cors_origins,omitempty"`
	CORSMethods        *[]string             `json:"cors_methods,omitempty"`
	CORSCredentials    *bool                 `json:"cors_credentials,omitempty"`
	BasicAuthUsers     *[]AppGWBasicAuthUser `json:"basic_auth_users,omitempty"`
	WAFPreset          *string               `json:"waf_preset,omitempty"`
	StripPrefix        *bool                 `json:"strip_prefix,omitempty"`
}
