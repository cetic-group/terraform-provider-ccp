package appgwtargetgroup

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strp(s string) *string { return &s }

func bucketAPI(prefix string, status, errMsg *string) client.AppGWTargetGroup {
	return client.AppGWTargetGroup{
		ID: "tg", AppGWID: "gw", Name: "assets", Algorithm: "roundrobin", HCProtocol: "http", HCMethod: "GET",
		HCPath: "/", HCExpectStatus: 200, HCIntervalSec: 5, HCTimeoutSec: 3, HCHealthyThreshold: 2, HCUnhealthyThreshold: 3,
		Kind: "bucket", BucketID: strp("bkt"), BucketPrefix: prefix, OriginStatus: status, OriginError: errMsg,
	}
}

func schemaOf(t *testing.T, r *tgResource) resource.SchemaResponse {
	var s resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &s)
	return s
}

func TestApplyToModelBucketAndLegacy(t *testing.T) {
	var m tgResourceModel
	applyToModel(&client.AppGWTargetGroup{ID: "x", Algorithm: "roundrobin"}, &m)
	if m.Kind.ValueString() != "upstream" || !m.BucketID.IsNull() || m.BucketPrefix.ValueString() != "" ||
		!m.OriginStatus.IsNull() || !m.OriginError.IsNull() {
		t.Fatalf("an API without bucket fields is an upstream pool: %+v", m)
	}
	api := bucketAPI("site", strp("error"), strp("bucket redevenu privé"))
	applyToModel(&api, &m)
	if m.Kind.ValueString() != "bucket" || m.BucketID.ValueString() != "bkt" || m.BucketPrefix.ValueString() != "site" ||
		m.OriginStatus.ValueString() != "error" || m.OriginError.ValueString() != "bucket redevenu privé" {
		t.Fatalf("bucket fields not mapped: %+v", m)
	}
}

func TestCreateBucketSendsOnlyBucketFields(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/app-gateways/gw/target-groups", Status: http.StatusCreated, Body: bucketAPI("site/assets", strp("ok"), nil)},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	sch := schemaOf(t, r)
	plan := tgResourceModel{
		ID: types.StringUnknown(), AppGWID: types.StringValue("gw"), Name: types.StringValue("assets"),
		Algorithm: types.StringValue("roundrobin"), HCProtocol: types.StringValue("http"), HCMethod: types.StringValue("GET"),
		HCPath: types.StringValue("/"), HCExpectStatus: types.Int64Value(200), HCIntervalSec: types.Int64Value(5),
		HCTimeoutSec: types.Int64Value(3), HCHealthyThreshold: types.Int64Value(2), HCUnhealthyThreshold: types.Int64Value(3),
		StickyEnabled: types.BoolValue(false), StickyCookieName: types.StringUnknown(),
		Kind: types.StringValue("bucket"), BucketID: types.StringValue("bkt"), BucketPrefix: types.StringValue("site/assets"),
		OriginStatus: types.StringUnknown(), OriginError: types.StringUnknown(),
	}
	p := tfsdk.Plan{Schema: sch.Schema}
	if d := p.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: p}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	calls := server.Calls()
	if len(calls) != 1 {
		t.Fatal(calls)
	}
	var payload map[string]any
	if err := json.Unmarshal(calls[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["kind"] != "bucket" || payload["bucket_id"] != "bkt" || payload["bucket_prefix"] != "site/assets" || payload["name"] != "assets" {
		t.Fatalf("bucket fields missing: %v", payload)
	}
	for _, f := range serverFieldAttrs {
		if _, found := payload[f]; found {
			t.Fatalf("%s must not be sent for a bucket target: %v", f, payload)
		}
	}
	var got tgResourceModel
	resp.State.Get(ctx, &got)
	if got.OriginStatus.ValueString() != "ok" || got.Kind.ValueString() != "bucket" {
		t.Fatalf("state not filled from the response: %+v", got)
	}
}

func TestCreateUpstreamPayloadUnchanged(t *testing.T) {
	ctx := context.Background()
	up := bucketAPI("", nil, nil)
	up.Kind, up.BucketID = "upstream", nil
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/app-gateways/gw/target-groups", Status: http.StatusCreated, Body: up},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	sch := schemaOf(t, r)
	plan := tgResourceModel{
		ID: types.StringUnknown(), AppGWID: types.StringValue("gw"), Name: types.StringValue("pool"),
		Algorithm: types.StringValue("leastconn"), HCProtocol: types.StringValue("http"), HCMethod: types.StringValue("GET"),
		HCPath: types.StringValue("/"), HCExpectStatus: types.Int64Value(200), HCIntervalSec: types.Int64Value(5),
		HCTimeoutSec: types.Int64Value(3), HCHealthyThreshold: types.Int64Value(2), HCUnhealthyThreshold: types.Int64Value(3),
		StickyEnabled: types.BoolValue(false), StickyCookieName: types.StringUnknown(),
		Kind: types.StringValue("upstream"), BucketID: types.StringNull(), BucketPrefix: types.StringValue(""),
		OriginStatus: types.StringUnknown(), OriginError: types.StringUnknown(),
	}
	p := tfsdk.Plan{Schema: sch.Schema}
	if d := p.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: p}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var payload map[string]any
	if err := json.Unmarshal(server.Calls()[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"kind", "bucket_id", "bucket_prefix"} {
		if _, found := payload[f]; found {
			t.Fatalf("an upstream create must not send %s: %v", f, payload)
		}
	}
	if payload["algorithm"] != "leastconn" {
		t.Fatalf("server fields still sent: %v", payload)
	}
}

func TestUpdateBucketSendsOnlyTheFolder(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/app-gateways/gw/target-groups/tg", Status: http.StatusOK, Body: bucketAPI("", nil, nil)},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	sch := schemaOf(t, r)
	var state tgResourceModel
	api := bucketAPI("site", strp("ok"), nil)
	applyToModel(&api, &state)
	plan := state
	plan.BucketPrefix = types.StringValue("")
	plan.OriginStatus, plan.OriginError = types.StringUnknown(), types.StringUnknown()
	st := tfsdk.State{Schema: sch.Schema}
	if d := st.Set(ctx, &state); d.HasError() {
		t.Fatal(d)
	}
	p := tfsdk.Plan{Schema: sch.Schema}
	if d := p.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.UpdateResponse{State: st}
	r.Update(ctx, resource.UpdateRequest{Plan: p, State: st}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var payload map[string]any
	if err := json.Unmarshal(server.Calls()[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	// Clearing the folder must be sent explicitly as an empty string.
	if v, found := payload["bucket_prefix"]; !found || v != "" || len(payload) != 1 {
		t.Fatalf("expected only an explicit empty bucket_prefix, got %v", payload)
	}
}

func TestCanonicalBucketPrefix(t *testing.T) {
	good := map[string]string{"": "", "a": "a", "a/b": "a/b", "/a/b/": "a/b", "a//b": "a/b", "v1.0/img_x-y": "v1.0/img_x-y", "..a": "..a"}
	for in, want := range good {
		got, err := canonicalBucketPrefix(in)
		if err != nil || got != want {
			t.Fatalf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, in := range []string{"a/./b", "../a", "a/..", "a b", "é", "a?x=1", "a\\b", "a%2e", string(make([]byte, 513))} {
		if _, err := canonicalBucketPrefix(in); err == nil {
			t.Fatalf("%q must be rejected", in)
		}
	}
}
