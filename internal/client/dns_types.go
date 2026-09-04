package client

// ─── Private DNS (zones served inside the customer's private network) ────────
//
// Backend contract: `apps/api/app/services/DNS_CONTRACT.md` (#1387).
//
// Two shapes matter here and neither is guessable from the resource schema:
//
//  1. The unit of edition is the **record set** — the (name, type) couple and
//     ALL of its values — not the individual record. `records` REPLACES the
//     whole list on every write; there is no incremental add.
//  2. `GET /v1/dns/zones` (list) returns the *summary* shape: no `resolver`,
//     no `ownership_challenge`. Only `GET /v1/dns/zones/{id}` carries them.
//     Anything that needs the resolver has to read the zone by ID — see
//     pitfall #5 in CLAUDE.md (mapping a field the response omits wipes it).

// DNS zone lifecycle states, as returned by the API.
const (
	DNSZoneStatusPendingVerification = "pending_verification"
	DNSZoneStatusProvisioning        = "provisioning"
	DNSZoneStatusActive              = "active"
	DNSZoneStatusError               = "error"
)

// DNSResolverEndpoint is one name-server address together with the subnet it
// serves. Guests must query the address of THEIR OWN subnet: every address
// answers the same zones, but each one is only reachable from its own subnet.
type DNSResolverEndpoint struct {
	Address  string  `json:"address"`
	VnetID   string  `json:"vnet_id"`
	VnetName string  `json:"vnet_name"`
	VnetCIDR *string `json:"vnet_cidr"`
}

// DNSResolverInfo is the read-only view of the name server that serves the
// zone. It is never created or destroyed on its own: it appears with the first
// zone of a private network and is torn down with the last one.
type DNSResolverInfo struct {
	Addresses  []string              `json:"addresses"`
	Endpoints  []DNSResolverEndpoint `json:"endpoints"`
	Tier       string                `json:"tier"`
	Status     string                `json:"status"`
	NsHostname string                `json:"ns_hostname"`
	// Guests get the name server at CREATION time. Turning private DNS on in a
	// network that already has machines does not reconfigure them.
	AppliesToNewGuestsOnly bool `json:"applies_to_new_guests_only"`
}

// DNSOwnershipChallenge is returned only while a PUBLIC domain name waits for
// its proof of ownership. Nil for internal suffixes (`.internal`, `.lan`, a
// single label…), which belong to nobody and have nothing to prove.
type DNSOwnershipChallenge struct {
	RecordName  string `json:"record_name"`
	RecordType  string `json:"record_type"`
	RecordValue string `json:"record_value"`
	Reason      string `json:"reason"`
}

// DNSZone is the zone as returned by the API. `Resolver` and
// `OwnershipChallenge` are only populated by the single-zone GET.
type DNSZone struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	VpcID           string  `json:"vpc_id"`
	Region          string  `json:"region"`
	Status          string  `json:"status"`
	DefaultTTL      int64   `json:"default_ttl"`
	DNSSECEnabled   bool    `json:"dnssec_enabled"`
	ErrorMessage    *string `json:"error_message"`
	CreatedAt       string  `json:"created_at"`
	RecordSetsCount *int64  `json:"record_sets_count"`

	Resolver           *DNSResolverInfo       `json:"resolver"`
	OwnershipChallenge *DNSOwnershipChallenge `json:"ownership_challenge"`
}

// DNSZoneCreateRequest is the POST body. `DefaultTTL` is a pointer on purpose:
// omitting it makes the platform apply its own configured default, and sending
// a hard-coded value from the provider would make that setting dead.
type DNSZoneCreateRequest struct {
	Name          string `json:"name"`
	VpcID         string `json:"vpc_id"`
	Tier          string `json:"tier,omitempty"`
	DefaultTTL    *int64 `json:"default_ttl,omitempty"`
	DNSSECEnabled bool   `json:"dnssec_enabled"`
}

// DNSRecordSet is a (name, type) couple and all of its values.
type DNSRecordSet struct {
	ID              string   `json:"id"`
	ZoneID          string   `json:"zone_id"`
	Name            string   `json:"name"`
	RecordType      string   `json:"record_type"`
	TTL             int64    `json:"ttl"`
	Records         []string `json:"records"`
	IsSystemManaged bool     `json:"is_system_managed"`
	CreatedAt       string   `json:"created_at"`
}

// DNSRecordSetCreateRequest accepts `Name` relative (`www`), absolute
// (`www.corp.internal`) or `@` for the apex. The API always answers with the
// fully-qualified form.
type DNSRecordSetCreateRequest struct {
	Name       string   `json:"name"`
	RecordType string   `json:"record_type"`
	TTL        int64    `json:"ttl"`
	Records    []string `json:"records"`
}

// DNSRecordSetUpdateRequest carries the desired truth: `Records` REPLACES the
// value list. `Name` and `RecordType` are absent because they identify the
// record set — changing them is a destroy + create, not a field.
type DNSRecordSetUpdateRequest struct {
	TTL     *int64   `json:"ttl,omitempty"`
	Records []string `json:"records,omitempty"`
}
