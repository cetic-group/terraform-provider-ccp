package client

import (
	"fmt"
	"net/http"
	"net/url"

	"context"
)

// ─── Private DNS zones — /v1/dns/zones ───────────────────────────────────────

// ListDNSZones returns the zones of the calling organisation. The summary
// shape does NOT carry `resolver`; call GetDNSZone when it is needed.
func (c *Client) ListDNSZones(ctx context.Context) ([]DNSZone, error) {
	var out []DNSZone
	if err := c.do(ctx, http.MethodGet, "/v1/dns/zones", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateDNSZone declares a zone and, with it, the name server of its private
// network when that network has none yet. A PUBLIC domain name comes back in
// `pending_verification` with an `ownership_challenge` and nothing provisioned.
func (c *Client) CreateDNSZone(ctx context.Context, req DNSZoneCreateRequest) (*DNSZone, error) {
	var out DNSZone
	if err := c.do(ctx, http.MethodPost, "/v1/dns/zones", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDNSZone reads the full zone record, including the resolver block and the
// ownership challenge when there is one.
func (c *Client) GetDNSZone(ctx context.Context, id string) (*DNSZone, error) {
	var out DNSZone
	if err := c.do(ctx, http.MethodGet, "/v1/dns/zones/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyDNSZone asks the platform to look for the ownership TXT record and,
// once it is found, to provision the zone. Replayable: an already-verified
// zone is returned as-is.
func (c *Client) VerifyDNSZone(ctx context.Context, id string) (*DNSZone, error) {
	var out DNSZone
	if err := c.do(ctx, http.MethodPost, "/v1/dns/zones/"+url.PathEscape(id)+"/verify", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDNSZone removes the zone. The API refuses (409) while the zone still
// carries records — deletion is not a cascade.
func (c *Client) DeleteDNSZone(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/dns/zones/"+url.PathEscape(id), nil, nil)
}

// ─── Record sets — /v1/dns/zones/{id}/records ────────────────────────────────

// ListDNSRecordSets returns every record set of the zone, including the ones
// the platform owns (`IsSystemManaged`).
func (c *Client) ListDNSRecordSets(ctx context.Context, zoneID string) ([]DNSRecordSet, error) {
	var out []DNSRecordSet
	path := "/v1/dns/zones/" + url.PathEscape(zoneID) + "/records"
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDNSRecordSet fetches a single record set.
//
// The API exposes no `GET /records/{id}` — only the collection. This is
// pitfall #6 of CLAUDE.md: assuming the individual GET exists yields a 405 on
// the FIRST refresh, long after Create looked fine. So: list + filter, and
// synthesise the 404 the caller's Read expects.
func (c *Client) GetDNSRecordSet(ctx context.Context, zoneID, recordSetID string) (*DNSRecordSet, error) {
	sets, err := c.ListDNSRecordSets(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	for i := range sets {
		if sets[i].ID == recordSetID {
			return &sets[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Method:     http.MethodGet,
		Path:       fmt.Sprintf("/v1/dns/zones/%s/records/%s", zoneID, recordSetID),
		Detail:     "dns record set not found",
	}
}

// FindDNSRecordSet locates a record set by its identity — the (name, type)
// couple. `name` is matched on the fully-qualified form the API returns, so
// the caller must qualify a relative name first. Used by ImportState.
func (c *Client) FindDNSRecordSet(ctx context.Context, zoneID, fqdn, recordType string) (*DNSRecordSet, error) {
	sets, err := c.ListDNSRecordSets(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	for i := range sets {
		if sets[i].Name == fqdn && sets[i].RecordType == recordType {
			return &sets[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Method:     http.MethodGet,
		Path:       fmt.Sprintf("/v1/dns/zones/%s/records", zoneID),
		Detail:     fmt.Sprintf("no %s record set named %q in zone %s", recordType, fqdn, zoneID),
	}
}

func (c *Client) CreateDNSRecordSet(ctx context.Context, zoneID string, req DNSRecordSetCreateRequest) (*DNSRecordSet, error) {
	var out DNSRecordSet
	path := "/v1/dns/zones/" + url.PathEscape(zoneID) + "/records"
	if err := c.do(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateDNSRecordSet(ctx context.Context, zoneID, recordSetID string, req DNSRecordSetUpdateRequest) (*DNSRecordSet, error) {
	var out DNSRecordSet
	path := "/v1/dns/zones/" + url.PathEscape(zoneID) + "/records/" + url.PathEscape(recordSetID)
	if err := c.do(ctx, http.MethodPatch, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteDNSRecordSet(ctx context.Context, zoneID, recordSetID string) error {
	path := "/v1/dns/zones/" + url.PathEscape(zoneID) + "/records/" + url.PathEscape(recordSetID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}
