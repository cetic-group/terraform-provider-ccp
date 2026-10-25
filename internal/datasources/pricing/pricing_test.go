package pricing

import (
	"context"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func readPrices(t *testing.T, body []map[string]any, resourceType, plan types.String, replicas ...int64) pricingModel {
	t.Helper()
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/billing/pricing-v2", Status: 200, Body: body},
	})
	defer server.Close()
	d := &pricingDataSource{client: client.New(server.URL, "ccp_test_unit", "test")}
	var schema datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schema)
	config := tfsdk.State{Schema: schema.Schema}
	filter := pricingModel{ResourceType: resourceType, Plan: plan}
	if len(replicas) > 0 {
		filter.ReplicaCount = types.Int64Value(replicas[0])
	}
	if diags := config.Set(ctx, &filter); diags.HasError() {
		t.Fatalf("config: %v", diags)
	}
	response := datasource.ReadResponse{State: tfsdk.State{Schema: schema.Schema}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Raw: config.Raw, Schema: schema.Schema}}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("read: %v", response.Diagnostics)
	}
	var result pricingModel
	if diags := response.State.Get(ctx, &result); diags.HasError() {
		t.Fatalf("state: %v", diags)
	}
	if server.PendingRoutes() != 0 {
		t.Fatal("pricing HTTP response was not consumed")
	}
	return result
}

func TestReadFractionalPricingAndServerEstimates(t *testing.T) {
	result := readPrices(t, []map[string]any{
		{"id": "fractional", "resource_type": "appgw", "plan": "small", "hourly_price_cents": 0.4096,
			"monthly_price_eur": 2.99, "yearly_price_eur": 35.88, "currency": "eur", "billing_dimension": "flat_hourly"},
		{"id": "other", "resource_type": "load_balancer", "plan": "small", "hourly_price_cents": 2,
			"monthly_price_eur": 14.6, "yearly_price_eur": 175.2, "currency": "eur"},
	}, types.StringValue("appgw"), types.StringValue("small"))
	if len(result.Items) != 1 {
		t.Fatalf("type/plan filter lost: %+v", result.Items)
	}
	item := result.Items[0]
	if !item.HourlyPriceCents.Equal(types.Float64Value(0.4096)) {
		t.Fatalf("fractional rate lost: %v", item.HourlyPriceCents)
	}
	// Copy the API estimates exactly; do not recalculate monthly/yearly values.
	if item.MonthlyPriceEUR.ValueFloat64() != 2.99 || item.YearlyPriceEUR.ValueFloat64() != 35.88 {
		t.Fatalf("server estimates changed: %v / %v", item.MonthlyPriceEUR, item.YearlyPriceEUR)
	}
}

func TestReadNullPeriodicPriceIsNotFreeZero(t *testing.T) {
	result := readPrices(t, []map[string]any{
		{"id": "usage", "resource_type": "object_egress", "hourly_price_cents": 1,
			"monthly_price_eur": nil, "yearly_price_eur": nil, "billing_dimension": "per_gb_egress", "is_free": false},
		{"id": "free", "resource_type": "appgw", "hourly_price_cents": 0,
			"monthly_price_eur": 0, "yearly_price_eur": 0, "billing_dimension": "flat_hourly", "is_free": true},
		{"id": "omitted", "resource_type": "object_storage", "hourly_price_cents": 1},
	}, types.StringNull(), types.StringNull())
	if len(result.Items) != 3 {
		t.Fatalf("expected all prices: %+v", result.Items)
	}
	for _, i := range []int{0, 2} {
		if !result.Items[i].MonthlyPriceEUR.IsNull() || !result.Items[i].YearlyPriceEUR.IsNull() {
			t.Fatalf("absent periodic amounts became free zero: %+v", result.Items[i])
		}
	}
	free := result.Items[1]
	if free.MonthlyPriceEUR.IsNull() || free.YearlyPriceEUR.IsNull() || !free.IsFree.ValueBool() ||
		free.MonthlyPriceEUR.ValueFloat64() != 0 || free.YearlyPriceEUR.ValueFloat64() != 0 {
		t.Fatalf("explicit free amounts must stay known zero: %+v", free)
	}
}

func TestReadReplicaPricingFilter(t *testing.T) {
	rows := []map[string]any{
		{"id": "single", "resource_type": "appgw", "plan": "small", "replica_count": 1, "hourly_price_cents": 0.4096},
		{"id": "ha", "resource_type": "appgw", "plan": "small", "replica_count": 2, "hourly_price_cents": 0.7},
		{"id": "cache-single", "resource_type": "appgw_cache", "plan": "small", "replica_count": 1, "hourly_price_cents": 0.01},
		{"id": "cache-ha", "resource_type": "appgw_cache", "plan": "small", "replica_count": 2, "hourly_price_cents": 0.015},
		{"id": "historical", "resource_type": "load_balancer", "plan": "small", "hourly_price_cents": 1},
		{"id": "other", "resource_type": "container", "plan": "small", "hourly_price_cents": 1},
	}
	cases := []struct {
		kind     string
		replicas int64
		id       string
		rate     float64
	}{
		{"appgw", 1, "single", 0.4096}, {"appgw", 2, "ha", 0.7},
		{"appgw_cache", 1, "cache-single", 0.01}, {"appgw_cache", 2, "cache-ha", 0.015},
		{"load_balancer", 2, "historical", 1},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			result := readPrices(t, rows, types.StringValue(tc.kind), types.StringValue("small"), tc.replicas)
			if len(result.Items) != 1 || result.Items[0].ID.ValueString() != tc.id {
				t.Fatalf("wrong variant: %+v", result.Items)
			}
			if result.Items[0].HourlyPriceCents.ValueFloat64() != tc.rate {
				t.Fatalf("rate must already be total: %+v", result.Items[0])
			}
			if tc.id == "historical" && !result.Items[0].ReplicaCount.IsNull() {
				t.Fatal("do not invent metadata absent from historical response")
			}
		})
	}
	for _, kind := range []string{"load_balancer", "container"} {
		result := readPrices(t, rows, types.StringValue(kind), types.StringValue("small"), 1)
		if len(result.Items) != 0 {
			t.Fatalf("single must not match legacy HA or unrelated family: %+v", result.Items)
		}
	}
	result := readPrices(t, rows, types.StringNull(), types.StringNull())
	if len(result.Items) != len(rows) || !result.Items[5].ReplicaCount.IsNull() {
		t.Fatal("unfiltered catalog must retain unrelated families with null count")
	}
}
