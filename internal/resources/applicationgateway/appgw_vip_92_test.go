package applicationgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func planModel(vip types.String) appgwResourceModel {
	n := types.StringNull()
	return appgwResourceModel{
		ID: types.StringUnknown(), Name: types.StringValue("web-appgw"),
		Region: types.StringValue("RNN"), Plan: types.StringValue("appgw-medium"),
		VpcID: types.StringValue("vpc-1"), VnetID: types.StringValue("vnet-1"),
		VpcName: types.StringUnknown(), VnetName: types.StringUnknown(), VnetCIDR: types.StringUnknown(),
		CustomErrorPage: n, PublicIPID: n, PublicIPAddress: types.StringUnknown(),
		PublicIPStatus: types.StringUnknown(), VIPAddress: vip, Status: types.StringUnknown(),
		ErrorMessage: types.StringUnknown(), ForceHTTPS: types.BoolValue(true),
		HSTSEnabled: types.BoolValue(false), HSTSMaxAge: types.Int64Value(31536000),
		GlobalRateLimitPerSec: types.Int64Null(),
		GlobalAllowCIDRs:      types.ListValueMust(types.StringType, nil),
		GlobalDenyCIDRs:       types.ListValueMust(types.StringType, nil),
		TrustProxyHeaders:     types.BoolValue(false),
		Tags:                  types.ListNull(types.StringType),
		CreatedAt:             types.StringUnknown(),
	}
}

func runCreate(t *testing.T, plan appgwResourceModel, vipFromAPI string) (map[string]any, appgwResourceModel) {
	t.Helper()
	sent, out, diags := runCreateDiags(t, plan, vipFromAPI, vipFromAPI)
	if diags.HasError() {
		t.Fatalf("create: %v", diags)
	}
	return sent, out
}

func runCreateDiags(t *testing.T, plan appgwResourceModel, postVIP, vipFromAPI string, replicas ...int64) (map[string]any, appgwResourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	id := "appgw-92"
	active := fixtureActive(id, map[string]any{"vip_address": vipFromAPI})
	creating := fixtureCreating(id)
	if postVIP != "" {
		creating["vip_address"] = postVIP
	}
	if len(replicas) > 0 {
		creating["replica_count"] = replicas[0]
		active["replica_count"] = replicas[0]
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/app-gateways", Status: http.StatusCreated, Body: creating},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: active},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: active},
	})
	defer srv.Close()
	r := &appgwResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	pl := tfsdk.State{Schema: sch.Schema}
	if d := pl.Set(ctx, &plan); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sch.Schema}}, resp)
	var sent map[string]any
	for _, c := range srv.Calls() {
		if c.Method == "POST" {
			if err := json.Unmarshal(c.Body, &sent); err != nil {
				t.Fatal(err)
			}
		}
	}
	var out appgwResourceModel
	resp.State.Get(ctx, &out)
	return sent, out, resp.Diagnostics
}

func TestCreate_SendsPinnedVIP(t *testing.T) {
	sent, out := runCreate(t, planModel(types.StringValue("10.0.1.200")), "10.0.1.200")
	if sent["vip_address"] != "10.0.1.200" {
		t.Fatalf("vip_address not sent: %v", sent)
	}
	if out.VIPAddress.ValueString() != "10.0.1.200" {
		t.Fatalf("state vip = %v", out.VIPAddress)
	}
}

func TestCreate_OmitsVIPWhenUnset(t *testing.T) {
	sent, out := runCreate(t, planModel(types.StringUnknown()), "10.0.1.254")
	if _, ok := sent["vip_address"]; ok {
		t.Fatalf("vip_address must be omitted when not configured: %v", sent)
	}
	if out.VIPAddress.ValueString() != "10.0.1.254" {
		t.Fatalf("platform-picked vip not stored: %v", out.VIPAddress)
	}
}

// A platform that ignores vip_address must not yield a silent wrong address.
func TestCreate_PlatformIgnoringVIPFailsLoudly(t *testing.T) {
	_, out, diags := runCreateDiags(t, planModel(types.StringValue("10.0.1.200")), "", "10.0.1.254")
	if !diags.HasError() {
		t.Fatal("expected an error when the platform ignores vip_address")
	}
	if out.ID.ValueString() == "" {
		t.Fatal("the created gateway must stay in state (tainted), not be stranded")
	}
}
