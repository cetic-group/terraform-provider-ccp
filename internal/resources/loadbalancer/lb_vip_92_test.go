package loadbalancer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// runCreate drives the real Create with `plan` and returns the POST body sent.
func runCreate(t *testing.T, plan lbResourceModel, vipFromAPI string) (map[string]any, lbResourceModel) {
	t.Helper()
	sent, out, diags := runCreateDiags(t, plan, vipFromAPI, vipFromAPI)
	if diags.HasError() {
		t.Fatalf("create: %v", diags)
	}
	return sent, out
}

func runCreateDiags(t *testing.T, plan lbResourceModel, postVIP, vipFromAPI string, replicas ...int64) (map[string]any, lbResourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	body := apiLB(nil)
	body["vip_address"] = vipFromAPI
	body["listeners"] = []any{}
	post := apiLB(nil)
	post["listeners"] = []any{}
	if postVIP != "" {
		post["vip_address"] = postVIP
	}
	if len(replicas) > 0 {
		post["replica_count"] = replicas[0]
		body["replica_count"] = replicas[0]
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/load-balancers", Status: 201, Body: post},
		{Method: "GET", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: body},
		{Method: "GET", Path: "/v1/load-balancers/" + lbID, Status: 200, Body: body},
	})
	defer srv.Close()
	r := &lbResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
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
	var out lbResourceModel
	resp.State.Get(ctx, &out)
	return sent, out, resp.Diagnostics
}

func TestCreate_SendsPinnedVIP(t *testing.T) {
	plan := baseModel(types.StringNull())
	plan.VIPAddress = types.StringValue("10.0.1.200")
	sent, out := runCreate(t, plan, "10.0.1.200")
	if sent["vip_address"] != "10.0.1.200" {
		t.Fatalf("vip_address not sent: %v", sent)
	}
	if out.VIPAddress.ValueString() != "10.0.1.200" {
		t.Fatalf("state vip = %v", out.VIPAddress)
	}
}

func TestCreate_OmitsVIPWhenUnset(t *testing.T) {
	plan := baseModel(types.StringNull())
	plan.VIPAddress = types.StringUnknown()
	sent, out := runCreate(t, plan, "10.0.1.254")
	if _, ok := sent["vip_address"]; ok {
		t.Fatalf("vip_address must be omitted when not configured: %v", sent)
	}
	if out.VIPAddress.ValueString() != "10.0.1.254" {
		t.Fatalf("platform-picked vip not stored: %v", out.VIPAddress)
	}
}

// A platform that ignores vip_address must not yield a silent wrong address.
func TestCreate_PlatformIgnoringVIPFailsLoudly(t *testing.T) {
	plan := baseModel(types.StringNull())
	plan.VIPAddress = types.StringValue("10.0.1.200")
	_, out, diags := runCreateDiags(t, plan, "", "10.0.1.254")
	if !diags.HasError() {
		t.Fatal("expected an error when the platform ignores vip_address")
	}
	if out.ID.ValueString() == "" {
		t.Fatal("the created load balancer must stay in state (tainted)")
	}
}
