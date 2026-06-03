package vpc

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func fixture(id, name, region string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"region":     region,
		"cidr":       "10.1.0.0/16",
		"vlan_id":    1234,
		"sdn_type":   "evpn",
		"status":     "active",
		"tags":       []string{},
		"created_at": "2026-05-25T10:00:00Z",
	}
}

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs/vpc-1", Status: http.StatusOK, Body: fixture("vpc-1", "prod", "RNN")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetVPC(context.Background(), "vpc-1")
	if err != nil {
		t.Fatalf("GetVPC: %v", err)
	}
	if got.Name != "prod" || got.Region != "RNN" {
		t.Errorf("unexpected: %+v", got)
	}
	if got.VlanID == nil || *got.VlanID != 1234 {
		t.Errorf("expected vlan_id=1234, got %v", got.VlanID)
	}
	if got.CIDR == nil || *got.CIDR != "10.1.0.0/16" {
		t.Errorf("expected cidr=10.1.0.0/16, got %v", got.CIDR)
	}
}

func TestLookupByNameRegion(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs", Status: http.StatusOK, Body: []map[string]any{
			fixture("vpc-a", "prod", "RNN"),
			fixture("vpc-b", "prod", "PAR"),
			fixture("vpc-c", "dev", "RNN"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListVPCs(context.Background())
	if err != nil {
		t.Fatalf("ListVPCs: %v", err)
	}
	var matches []int
	for i := range list {
		if list[i].Name == "prod" && list[i].Region == "RNN" {
			matches = append(matches, i)
		}
	}
	if len(matches) != 1 || list[matches[0]].ID != "vpc-a" {
		t.Errorf("expected exactly 1 match for (prod, RNN) = vpc-a, got matches=%v", matches)
	}
}

func TestNotFound(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs/missing", Status: http.StatusNotFound, Body: map[string]any{"detail": "VPC not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	_, err := c.GetVPC(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !client.IsNotFound(err) {
		t.Errorf("expected IsNotFound, got %v", err)
	}
}
