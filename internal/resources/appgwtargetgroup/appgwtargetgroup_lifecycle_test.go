package appgwtargetgroup

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const listPath = "/v1/app-gateways/gw/target-groups"

func upstreamAPI() client.AppGWTargetGroup {
	tg := bucketAPI("", nil, nil)
	tg.Kind, tg.BucketID = "upstream", nil
	return tg
}

func stateOf(t *testing.T, r *tgResource, m tgResourceModel) tfsdk.State {
	t.Helper()
	sch := schemaOf(t, r)
	st := tfsdk.State{Schema: sch.Schema}
	if d := st.Set(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return st
}

func TestReadBucketTargetFillsBucketFields(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: listPath, Body: []client.AppGWTargetGroup{bucketAPI("site", strp("drift"), strp("raison"))}},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	// A state written before bucket targets existed: the new fields are null.
	old := tgResourceModel{ID: types.StringValue("tg"), AppGWID: types.StringValue("gw"), Name: types.StringValue("assets"),
		StickyCookieName: types.StringNull(), Kind: types.StringNull(), BucketID: types.StringNull(),
		BucketPrefix: types.StringNull(), OriginStatus: types.StringNull(), OriginError: types.StringNull()}
	resp := resource.ReadResponse{State: stateOf(t, r, old)}
	r.Read(ctx, resource.ReadRequest{State: stateOf(t, r, old)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got tgResourceModel
	resp.State.Get(ctx, &got)
	if got.Kind.ValueString() != "bucket" || got.BucketID.ValueString() != "bkt" || got.BucketPrefix.ValueString() != "site" ||
		got.OriginStatus.ValueString() != "drift" || got.OriginError.ValueString() != "raison" {
		t.Fatalf("bucket fields not read: %+v", got)
	}
}

func TestReadMissingTargetGroupRemovesItFromState(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: listPath, Body: []client.AppGWTargetGroup{}},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	var m tgResourceModel
	api := bucketAPI("site", nil, nil)
	applyToModel(&api, &m)
	resp := resource.ReadResponse{State: stateOf(t, r, m)}
	r.Read(ctx, resource.ReadRequest{State: stateOf(t, r, m)}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatal("a target group that no longer exists must be removed from the state")
	}
}

func TestImportThenRead(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: listPath, Body: []client.AppGWTargetGroup{bucketAPI("a/b", strp("ok"), nil)}},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	sch := schemaOf(t, r)
	imp := resource.ImportStateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: emptyRaw(t, r)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "gw/tg"}, &imp)
	if imp.Diagnostics.HasError() {
		t.Fatal(imp.Diagnostics)
	}
	rd := resource.ReadResponse{State: imp.State}
	r.Read(ctx, resource.ReadRequest{State: imp.State}, &rd)
	if rd.Diagnostics.HasError() {
		t.Fatal(rd.Diagnostics)
	}
	var got tgResourceModel
	rd.State.Get(ctx, &got)
	if got.Kind.ValueString() != "bucket" || got.BucketPrefix.ValueString() != "a/b" || got.OriginStatus.ValueString() != "ok" {
		t.Fatalf("import + read incomplete: %+v", got)
	}
	bad := resource.ImportStateResponse{State: tfsdk.State{Schema: sch.Schema, Raw: emptyRaw(t, r)}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "only-one-part"}, &bad)
	if !bad.Diagnostics.HasError() {
		t.Fatal("a malformed import ID must be refused")
	}
}

func TestCreatePrivateBucketIsRefusedWithDiagnostic(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: listPath, Status: http.StatusUnprocessableEntity,
			Body: map[string]string{"detail": "Ce bucket est privé."}},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	sch := schemaOf(t, r)
	plan := tgResourceModel{ID: types.StringUnknown(), AppGWID: types.StringValue("gw"), Name: types.StringValue("assets"),
		Kind: types.StringValue("bucket"), BucketID: types.StringValue("bkt"), BucketPrefix: types.StringValue(""),
		StickyCookieName: types.StringUnknown(), OriginStatus: types.StringUnknown(), OriginError: types.StringUnknown()}
	p := tfsdk.Plan{Schema: sch.Schema}
	if d := p.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sch.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: p}, &resp)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "privé") {
		t.Fatalf("expected a diagnostic carrying the platform message, got %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Fatal("no state may be stored when creation failed")
	}
}

func TestUpdateUpstreamNeverSendsBucketFields(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: listPath + "/tg", Body: upstreamAPI()},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	var state tgResourceModel
	api := upstreamAPI()
	applyToModel(&api, &state)
	plan := state
	plan.Name = types.StringValue("renamed")
	plan.Algorithm = types.StringValue("leastconn")
	plan.OriginStatus, plan.OriginError = types.StringUnknown(), types.StringUnknown()
	sch := schemaOf(t, r)
	p := tfsdk.Plan{Schema: sch.Schema}
	p.Set(ctx, &plan)
	st := stateOf(t, r, state)
	resp := resource.UpdateResponse{State: st}
	r.Update(ctx, resource.UpdateRequest{Plan: p, State: st}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var payload map[string]any
	json.Unmarshal(server.Calls()[0].Body, &payload)
	for _, f := range []string{"kind", "bucket_id", "bucket_prefix"} {
		if _, found := payload[f]; found {
			t.Fatalf("%s must not be sent for an upstream target group: %v", f, payload)
		}
	}
	if payload["algorithm"] != "leastconn" || payload["name"] != "renamed" {
		t.Fatalf("server fields missing: %v", payload)
	}
}

// The branch is chosen from the stored kind, not from the plan: a plan whose
// kind is not known yet (state written before `kind` existed and modified for
// another reason) must still never send server settings for a bucket target.
func TestUpdateBranchFollowsStoredKind(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: listPath + "/tg", Body: bucketAPI("site", nil, nil)},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	var state tgResourceModel
	api := bucketAPI("site", strp("ok"), nil)
	applyToModel(&api, &state)
	plan := state
	plan.Name = types.StringValue("renamed")
	plan.Kind = types.StringUnknown()
	plan.Algorithm = types.StringUnknown()
	plan.OriginStatus, plan.OriginError = types.StringUnknown(), types.StringUnknown()
	sch := schemaOf(t, r)
	p := tfsdk.Plan{Schema: sch.Schema}
	p.Set(ctx, &plan)
	st := stateOf(t, r, state)
	resp := resource.UpdateResponse{State: st}
	r.Update(ctx, resource.UpdateRequest{Plan: p, State: st}, &resp)
	var payload map[string]any
	json.Unmarshal(server.Calls()[0].Body, &payload)
	if _, found := payload["algorithm"]; found || payload["name"] != "renamed" {
		t.Fatalf("a bucket target must only send name/bucket fields: %v", payload)
	}
}

// A legacy state that only gains `kind = "upstream"` has nothing to send: the
// platform must not receive an empty PATCH.
func TestUpdateWithNothingToSendReadsInstead(t *testing.T) {
	ctx := context.Background()
	server := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: listPath, Body: []client.AppGWTargetGroup{upstreamAPI()}},
	})
	defer server.Close()
	r := &tgResource{client: client.New(server.URL, "test", "test")}
	var state tgResourceModel
	api := upstreamAPI()
	applyToModel(&api, &state)
	state.Kind, state.BucketPrefix = types.StringNull(), types.StringNull()
	plan := state
	plan.Kind, plan.BucketPrefix = types.StringValue("upstream"), types.StringValue("")
	sch := schemaOf(t, r)
	p := tfsdk.Plan{Schema: sch.Schema}
	p.Set(ctx, &plan)
	st := stateOf(t, r, state)
	resp := resource.UpdateResponse{State: st}
	r.Update(ctx, resource.UpdateRequest{Plan: p, State: st}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, c := range server.Calls() {
		if c.Method == "PATCH" {
			t.Fatal("an empty PATCH must not be sent")
		}
	}
	var got tgResourceModel
	resp.State.Get(ctx, &got)
	if got.Kind.ValueString() != "upstream" {
		t.Fatalf("kind not stored: %+v", got)
	}
}

func emptyRaw(t *testing.T, r *tgResource) tftypes.Value {
	t.Helper()
	sch := schemaOf(t, r)
	return tftypes.NewValue(sch.Schema.Type().TerraformType(context.Background()), nil)
}
