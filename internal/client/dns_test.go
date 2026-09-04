// Tests for the private DNS client (#1387).
//
// They exercise the two shapes of the API that the resource layer depends on
// and that no schema can express: there is no individual GET on a record set,
// and `records` is sent whole on every write.
package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func newClient(url string) *client.Client {
	return client.New(url, "ccp_test_unit", "0.0.0-test")
}

func zoneBody(status string) map[string]any {
	return map[string]any{
		"id": "z-1", "name": "corp.internal", "vpc_id": "vpc-1", "region": "RNN",
		"status": status, "default_ttl": 3600, "dnssec_enabled": false,
		"created_at": "2026-09-01T10:00:00Z", "record_sets_count": 2,
		"resolver": map[string]any{
			"addresses": []string{"10.20.0.51", "10.21.0.51"},
			"endpoints": []map[string]any{
				{"address": "10.20.0.51", "vnet_id": "vn-1", "vnet_name": "office", "vnet_cidr": "10.20.0.0/24"},
				{"address": "10.21.0.51", "vnet_id": "vn-2", "vnet_name": "workshop", "vnet_cidr": "10.21.0.0/24"},
			},
			"tier": "prod", "status": "active",
			"ns_hostname": "ns1.dns.example", "applies_to_new_guests_only": true,
		},
	}
}

// The resolver block only exists on the single-zone GET, and it is the whole
// reason the provider reads a zone back instead of trusting the list.
func TestGetDNSZone_CarriesResolverEndpoints(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/dns/zones/z-1", Status: http.StatusOK, Body: zoneBody("active")},
	})
	defer srv.Close()

	z, err := newClient(srv.URL).GetDNSZone(context.Background(), "z-1")
	if err != nil {
		t.Fatalf("GetDNSZone: %v", err)
	}
	if z.Resolver == nil {
		t.Fatal("resolver block dropped")
	}
	if len(z.Resolver.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(z.Resolver.Endpoints))
	}
	if z.Resolver.Endpoints[1].VnetID != "vn-2" || z.Resolver.Endpoints[1].Address != "10.21.0.51" {
		t.Fatalf("endpoint not paired with its subnet: %+v", z.Resolver.Endpoints[1])
	}
	if z.Resolver.Endpoints[0].VnetCIDR == nil || *z.Resolver.Endpoints[0].VnetCIDR != "10.20.0.0/24" {
		t.Fatalf("vnet_cidr dropped: %+v", z.Resolver.Endpoints[0])
	}
}

// `default_ttl` nil must not reach the wire: sending a hard-coded value would
// make the platform-wide setting dead.
func TestCreateDNSZone_OmitsUnsetDefaultTTL(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/dns/zones", Status: http.StatusCreated, Body: zoneBody("provisioning")},
	})
	defer srv.Close()

	_, err := newClient(srv.URL).CreateDNSZone(context.Background(), client.DNSZoneCreateRequest{
		Name: "corp.internal", VpcID: "vpc-1", Tier: "prod",
	})
	if err != nil {
		t.Fatalf("CreateDNSZone: %v", err)
	}
	var sent map[string]any
	if err := json.Unmarshal(srv.Calls()[0].Body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if _, present := sent["default_ttl"]; present {
		t.Errorf("default_ttl sent although unset: %s", srv.Calls()[0].Body)
	}
	if sent["vpc_id"] != "vpc-1" {
		t.Errorf("the zone must be scoped to the private network, got %v", sent["vpc_id"])
	}
	// `dnssec_enabled` has no omitempty: false is a value the API must receive.
	if v, ok := sent["dnssec_enabled"]; !ok || v != false {
		t.Errorf("dnssec_enabled must be sent explicitly, got %v (%t)", v, ok)
	}
}

func TestCreateDNSZone_SendsDefaultTTLWhenSet(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/dns/zones", Status: http.StatusCreated, Body: zoneBody("provisioning")},
	})
	defer srv.Close()

	ttl := int64(300)
	if _, err := newClient(srv.URL).CreateDNSZone(context.Background(), client.DNSZoneCreateRequest{
		Name: "corp.internal", VpcID: "vpc-1", DefaultTTL: &ttl,
	}); err != nil {
		t.Fatalf("CreateDNSZone: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(srv.Calls()[0].Body, &sent)
	if sent["default_ttl"] != float64(300) {
		t.Errorf("default_ttl not forwarded: %s", srv.Calls()[0].Body)
	}
}

func recordSets() []map[string]any {
	return []map[string]any{
		{
			"id": "r-1", "zone_id": "z-1", "name": "www.corp.internal", "record_type": "A",
			"ttl": 300, "records": []string{"10.20.0.10"}, "is_system_managed": false,
			"created_at": "2026-09-01T10:00:00Z",
		},
		{
			"id": "r-2", "zone_id": "z-1", "name": "corp.internal", "record_type": "NS",
			"ttl": 3600, "records": []string{"ns1.dns.example."}, "is_system_managed": true,
			"created_at": "2026-09-01T10:00:00Z",
		},
	}
}

// The API exposes no `GET /records/{id}` — only the collection. Assuming
// otherwise yields a 405 on the FIRST refresh, long after Create looked fine
// (CLAUDE.md pitfall #6). The client must list and filter, and it must
// synthesise the 404 that Read relies on to drop the resource from state.
func TestGetDNSRecordSet_ListsAndFilters(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/dns/zones/z-1/records", Status: http.StatusOK, Body: recordSets()},
	})
	defer srv.Close()

	rs, err := newClient(srv.URL).GetDNSRecordSet(context.Background(), "z-1", "r-1")
	if err != nil {
		t.Fatalf("GetDNSRecordSet: %v", err)
	}
	if rs.Name != "www.corp.internal" {
		t.Fatalf("wrong record set: %+v", rs)
	}
	if got := srv.Calls()[0].Path; got != "/v1/dns/zones/z-1/records" {
		t.Errorf("expected a collection GET, got %s", got)
	}
}

func TestGetDNSRecordSet_MissingIsNotFound(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/dns/zones/z-1/records", Status: http.StatusOK, Body: recordSets()},
	})
	defer srv.Close()

	_, err := newClient(srv.URL).GetDNSRecordSet(context.Background(), "z-1", "r-does-not-exist")
	if !client.IsNotFound(err) {
		t.Fatalf("expected a 404 so Read drops the resource, got %v", err)
	}
}

// Import resolves a record by its identity, the (fully qualified name, type)
// couple — not by a UUID the user does not have.
func TestFindDNSRecordSet_MatchesNameAndType(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/dns/zones/z-1/records", Status: http.StatusOK, Body: recordSets()},
		{Method: "GET", Path: "/v1/dns/zones/z-1/records", Status: http.StatusOK, Body: recordSets()},
	})
	defer srv.Close()
	c := newClient(srv.URL)

	rs, err := c.FindDNSRecordSet(context.Background(), "z-1", "www.corp.internal", "A")
	if err != nil {
		t.Fatalf("FindDNSRecordSet: %v", err)
	}
	if rs.ID != "r-1" {
		t.Fatalf("wrong record set: %+v", rs)
	}
	// Same name, different type: a record is identified by BOTH.
	if _, err := c.FindDNSRecordSet(context.Background(), "z-1", "www.corp.internal", "TXT"); !client.IsNotFound(err) {
		t.Fatalf("name alone must not match, got %v", err)
	}
}

// `records` REPLACES the value list — the API rewrites the couple whole, which
// is exactly Terraform's model. The test reads what was SENT, not only what
// came back: a client that computed a delta would pass a response-only check.
func TestUpdateDNSRecordSet_SendsTheWholeValueSet(t *testing.T) {
	updated := map[string]any{
		"id": "r-1", "zone_id": "z-1", "name": "www.corp.internal", "record_type": "A",
		"ttl": 600, "records": []string{"10.20.0.11", "10.20.0.12"},
		"is_system_managed": false, "created_at": "2026-09-01T10:00:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/dns/zones/z-1/records/r-1", Status: http.StatusOK, Body: updated},
	})
	defer srv.Close()

	ttl := int64(600)
	if _, err := newClient(srv.URL).UpdateDNSRecordSet(context.Background(), "z-1", "r-1",
		client.DNSRecordSetUpdateRequest{TTL: &ttl, Records: []string{"10.20.0.11", "10.20.0.12"}}); err != nil {
		t.Fatalf("UpdateDNSRecordSet: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(srv.Calls()[0].Body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	values, _ := sent["records"].([]any)
	if len(values) != 2 || values[0] != "10.20.0.11" || values[1] != "10.20.0.12" {
		t.Errorf("the desired set must be sent whole, got %s", srv.Calls()[0].Body)
	}
	// `name` and `record_type` identify the record: sending them would ask the
	// API for something it has no route to do.
	if _, present := sent["name"]; present {
		t.Errorf("name must not be sent on update: %s", srv.Calls()[0].Body)
	}
	if _, present := sent["record_type"]; present {
		t.Errorf("record_type must not be sent on update: %s", srv.Calls()[0].Body)
	}
}

// A zone that still carries records is refused, and the message names what to
// do. The provider relays it, so the client must surface a 409 as such.
func TestDeleteDNSZone_ConflictIsRecognisable(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/dns/zones/z-1", Status: http.StatusConflict,
			Body: map[string]any{"detail": "La zone porte encore des enregistrements."}},
	})
	defer srv.Close()

	err := newClient(srv.URL).DeleteDNSZone(context.Background(), "z-1")
	if !client.IsConflict(err) {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if !contains(err.Error(), "porte encore des enregistrements") {
		t.Errorf("the API message must survive: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
