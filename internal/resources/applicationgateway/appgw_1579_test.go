// Tests #1579 (issue #87) : page d'indisponibilité et champs réseau de
// ccp_application_gateway — le vrai Update du framework contre un serveur
// simulé, en vérifiant ce qui est ENVOYÉ.
package applicationgateway

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func gwModel(id string, page types.String) appgwResourceModel {
	empty := types.ListValueMust(types.StringType, nil)
	return appgwResourceModel{
		ID: types.StringValue(id), Name: types.StringValue("web-appgw"), Region: types.StringValue("RNN"),
		Plan: types.StringValue("appgw-medium"), VpcID: types.StringValue("vpc-1"), VnetID: types.StringValue("vnet-1"),
		VpcName: types.StringNull(), VnetName: types.StringNull(), VnetCIDR: types.StringNull(),
		CustomErrorPage: page,
		PublicIPID:      types.StringNull(), PublicIPAddress: types.StringNull(), PublicIPStatus: types.StringNull(),
		VIPAddress: types.StringValue("10.0.1.10"), Status: types.StringValue("active"), ErrorMessage: types.StringNull(),
		ForceHTTPS: types.BoolValue(true), HSTSEnabled: types.BoolValue(false), HSTSMaxAge: types.Int64Value(31536000),
		GlobalRateLimitPerSec: types.Int64Null(), GlobalAllowCIDRs: empty, GlobalDenyCIDRs: empty,
		TrustProxyHeaders: types.BoolValue(false),
		Tags:              types.ListValueMust(types.StringType, []attr.Value{types.StringValue("env:prod")}),
		CreatedAt:         types.StringValue("2026-05-15T10:00:00Z"),
	}
}

func runGwUpdate(t *testing.T, srv *testutil.Server, state, plan appgwResourceModel) (appgwResourceModel, *resource.UpdateResponse) {
	t.Helper()
	ctx := context.Background()
	r := &appgwResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	st := tfsdk.State{Schema: sch.Schema}
	if d := st.Set(ctx, &state); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	pl := tfsdk.State{Schema: sch.Schema}
	if d := pl.Set(ctx, &plan); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Raw: st.Raw, Schema: sch.Schema}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: pl.Raw, Schema: sch.Schema},
		State: tfsdk.State{Raw: st.Raw, Schema: sch.Schema},
	}, resp)
	var out appgwResourceModel
	if !resp.Diagnostics.HasError() {
		resp.State.Get(ctx, &out)
	}
	return out, resp
}

func TestUpdate_RemovingTheErrorPageSendsNull(t *testing.T) {
	id := "appgw-1579"
	net := map[string]any{"vpc_name": "prod", "vnet_name": "front", "vnet_cidr": "10.0.1.0/24"}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: fixtureActive(id, net)},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK, Body: fixtureActive(id, net)},
	})
	defer srv.Close()

	out, resp := runGwUpdate(t, srv, gwModel(id, types.StringValue("<h1>maintenance</h1>")), gwModel(id, types.StringNull()))
	if resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	var patch string
	for _, c := range srv.Calls() {
		if c.Method == "PATCH" {
			patch = string(c.Body)
		}
	}
	if patch != `{"custom_error_page_html":null}` {
		t.Fatalf("retrait : PATCH attendu {\"custom_error_page_html\":null}, envoyé %s", patch)
	}
	if !out.CustomErrorPage.IsNull() {
		t.Fatalf("état après retrait : null attendu, got %v", out.CustomErrorPage)
	}
	if out.VpcName.ValueString() != "prod" || out.VnetCIDR.ValueString() != "10.0.1.0/24" || out.VnetName.ValueString() != "front" {
		t.Fatalf("champs réseau non relus : %v %v %v", out.VpcName, out.VnetName, out.VnetCIDR)
	}
}

func TestUpdate_UnchangedPageIsNotSent(t *testing.T) {
	id := "appgw-1579b"
	page := "<h1>x</h1>"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/app-gateways/" + id, Status: http.StatusOK,
			Body: fixtureActive(id, map[string]any{"custom_error_page_html": page, "tags": []string{"env:staging"}})},
		{Method: "GET", Path: "/v1/app-gateways/" + id, Status: http.StatusOK,
			Body: fixtureActive(id, map[string]any{"custom_error_page_html": page, "tags": []string{"env:staging"}})},
	})
	defer srv.Close()
	plan := gwModel(id, types.StringValue(page))
	plan.Tags = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("env:staging")})
	if _, resp := runGwUpdate(t, srv, gwModel(id, types.StringValue(page)), plan); resp.Diagnostics.HasError() {
		t.Fatalf("update: %v", resp.Diagnostics)
	}
	for _, c := range srv.Calls() {
		if c.Method == "PATCH" && string(c.Body) != `{"tags":["env:staging"]}` {
			t.Fatalf("page inchangée : elle ne doit pas partir, envoyé %s", c.Body)
		}
	}
}
