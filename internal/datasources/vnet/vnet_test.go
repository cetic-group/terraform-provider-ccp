package vnet

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func fixture(id, vpcID, name string) map[string]any {
	return map[string]any{
		"id":         id,
		"vpc_id":     vpcID,
		"name":       name,
		"cidr":       "10.20.0.0/24",
		"gateway":    "10.20.0.1",
		"dhcp_start": "10.20.0.100",
		"dhcp_end":   "10.20.0.200",
		"snat":       true,
		"isolated":   false,
		"status":     "active",
		"tags":       []string{},
		"created_at": "2026-05-25T10:00:00Z",
	}
}

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs/vpc-1/vnets", Status: http.StatusOK, Body: []map[string]any{
			fixture("vnet-1", "vpc-1", "frontend"),
			fixture("vnet-2", "vpc-1", "backend"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetVNet(context.Background(), "vpc-1", "vnet-1")
	if err != nil {
		t.Fatalf("GetVNet: %v", err)
	}
	if got.Name != "frontend" {
		t.Errorf("expected frontend, got %q", got.Name)
	}
	if got.Gateway == nil || *got.Gateway != "10.20.0.1" {
		t.Errorf("expected gateway 10.20.0.1, got %v", got.Gateway)
	}
}

func TestLookupByName(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs/vpc-1/vnets", Status: http.StatusOK, Body: []map[string]any{
			fixture("vnet-a", "vpc-1", "frontend"),
			fixture("vnet-b", "vpc-1", "backend"),
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListVNets(context.Background(), "vpc-1")
	if err != nil {
		t.Fatalf("ListVNets: %v", err)
	}
	var found *client.VNet
	for i := range list {
		if list[i].Name == "backend" {
			found = &list[i]
			break
		}
	}
	if found == nil || found.ID != "vnet-b" {
		t.Errorf("expected vnet-b, got %+v", found)
	}
}
