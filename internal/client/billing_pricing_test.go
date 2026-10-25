package client

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestListPricingFractionalRates(t *testing.T) {
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/billing/pricing-v2", Status: 200, Body: []map[string]any{
			{"id": "fractional", "resource_type": "appgw", "hourly_price_cents": 0.4096},
			{"id": "whole", "resource_type": "load_balancer", "hourly_price_cents": 2},
		}},
	})
	defer server.Close()
	rows, err := New(server.URL, "ccp_test_unit", "test").ListPricing(context.Background())
	if err != nil {
		t.Fatalf("fractional API rate must decode: %v", err)
	}
	if len(rows) != 2 || float64(rows[0].HourlyPriceCents) != 0.4096 || float64(rows[1].HourlyPriceCents) != 2 {
		t.Fatalf("hourly prices changed: %+v", rows)
	}
}
