package objectbucket

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// Versioning (cetic-cloud-platform#1923) : bloc opt-in comme le CORS, jamais
// désactivé, durée envoyée seulement quand elle est configurée.

const versioningPath = "/v1/buckets/" + bucketID + "/versioning"

func vers(status string, days *int64) types.Object {
	d := types.Int64Null()
	if days != nil {
		d = types.Int64Value(*days)
	}
	return versioningToObject(types.StringValue(status), d)
}

func versUnknownDays(status string) types.Object {
	return versioningToObject(types.StringValue(status), types.Int64Unknown())
}

func i64(v int64) *int64 { return &v }

func withVersioning(m objectBucketResourceModel, v types.Object) objectBucketResourceModel {
	m.Versioning = v
	return m
}

func versioningBody(status any, days any, sync string) map[string]any {
	return map[string]any{"status": status, "noncurrent_version_expiration_days": days, "sync_status": sync}
}

// putCapture records every versioning PUT body and answers with `answer`.
func putCapture(bodies *[]map[string]any, answer map[string]any) testutil.Route {
	return testutil.Route{Method: "PUT", Path: versioningPath, Status: 200,
		BodyFn: func(t *testing.T, body []byte) (int, any) {
			var got map[string]any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			*bodies = append(*bodies, got)
			return 200, answer
		}}
}

func bucketRoutes(extra ...testutil.Route) testutil.Routes {
	return append(testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(false)},
		credsRoute(),
	}, extra...)
}

func versioningOf(t *testing.T, s tfsdk.State) *versioningModel {
	t.Helper()
	var o types.Object
	if d := s.GetAttribute(context.Background(), path.Root("versioning"), &o); d.HasError() {
		t.Fatal(d)
	}
	m, d := versioningFromObject(context.Background(), o)
	if d.HasError() {
		t.Fatal(d)
	}
	return m
}

func noVersioningCall(t *testing.T, srv *testutil.Server) {
	t.Helper()
	for _, p := range paths(srv) {
		if strings.Contains(p, "/versioning") {
			t.Fatalf("a /versioning call was made for a bucket without versioning block: %v", paths(srv))
		}
	}
}

// ── Schéma ──────────────────────────────────────────────────────────────────

func TestSchema_VersioningBlock(t *testing.T) {
	var sc resource.SchemaResponse
	(&objectBucketResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sc)
	block, ok := sc.Schema.Blocks["versioning"].(rschema.SingleNestedBlock)
	if !ok {
		t.Fatal("versioning must be a single nested block")
	}
	// ⚠️ Required inside a single nested block is enforced even when the block is
	// absent (every bucket without versioning would be refused): the block requires
	// `status`, the attribute itself stays optional.
	status := block.Attributes["status"].(rschema.StringAttribute)
	if status.Required || len(status.Validators) == 0 || len(block.Validators) == 0 {
		t.Fatal("status must be optional, limited to Enabled / Suspended, and required by the block")
	}
	days := block.Attributes["noncurrent_version_expiration_days"].(rschema.Int64Attribute)
	if !days.Optional || !days.Computed || len(days.Validators) == 0 || len(days.PlanModifiers) == 0 {
		t.Fatal("retention must be optional, computed, bounded, and keep the state value when unset")
	}
}

// ── Corps envoyé ────────────────────────────────────────────────────────────

func TestVersioningRequest_RetentionOnlyWhenSet(t *testing.T) {
	unset := versioningRequest(&versioningModel{Status: types.StringValue("Enabled"), NoncurrentVersionExpirationDays: types.Int64Unknown()})
	if unset.NoncurrentVersionExpirationDays != nil {
		t.Fatal("an unset retention must not be sent: the platform keeps the value in place")
	}
	null := versioningRequest(&versioningModel{Status: types.StringValue("Enabled"), NoncurrentVersionExpirationDays: types.Int64Null()})
	if null.NoncurrentVersionExpirationDays != nil {
		t.Fatal("a null retention must not be sent")
	}
	set := versioningRequest(&versioningModel{Status: types.StringValue("Suspended"), NoncurrentVersionExpirationDays: types.Int64Value(90)})
	if set.Status != "Suspended" || set.NoncurrentVersionExpirationDays == nil || *set.NoncurrentVersionExpirationDays != 90 {
		t.Fatalf("got %+v", set)
	}
}

func TestVersioningNeedsWrite(t *testing.T) {
	m := func(s string, d types.Int64) *versioningModel {
		return &versioningModel{Status: types.StringValue(s), NoncurrentVersionExpirationDays: d}
	}
	cases := []struct {
		name        string
		plan, state *versioningModel
		want        bool
	}{
		{"no block", nil, m("Enabled", types.Int64Value(30)), false},
		{"block added", m("Enabled", types.Int64Unknown()), nil, true},
		{"status changed", m("Suspended", types.Int64Value(30)), m("Enabled", types.Int64Value(30)), true},
		{"retention changed", m("Enabled", types.Int64Value(90)), m("Enabled", types.Int64Value(30)), true},
		{"retention unset", m("Enabled", types.Int64Unknown()), m("Enabled", types.Int64Value(30)), false},
		{"unchanged", m("Enabled", types.Int64Value(30)), m("Enabled", types.Int64Value(30)), false},
	}
	for _, c := range cases {
		if got := versioningNeedsWrite(c.plan, c.state); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// ── Update ──────────────────────────────────────────────────────────────────

func TestUpdate_WithoutVersioningBlock_NoCall(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes(
		testutil.Route{Method: "PATCH", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(true)},
	))
	defer srv.Close()
	resp := runUpdate(t, srv, model(false, nullRules()), model(true, nullRules()))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	noVersioningCall(t, srv)
}

func TestUpdate_AddingTheBlock_EnablesWithoutRetention(t *testing.T) {
	var bodies []map[string]any
	srv := testutil.NewTestServer(t, bucketRoutes(putCapture(&bodies, versioningBody("Enabled", 30, "applied"))))
	defer srv.Close()
	plan := withVersioning(model(false, nullRules()), versUnknownDays("Enabled"))
	resp := runUpdate(t, srv, model(false, nullRules()), plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(bodies) != 1 || bodies[0]["status"] != "Enabled" {
		t.Fatalf("PUT bodies = %v", bodies)
	}
	if _, sent := bodies[0]["noncurrent_version_expiration_days"]; sent {
		t.Fatalf("an unset retention must not be sent: %v", bodies[0])
	}
	got := versioningOf(t, resp.State)
	if got == nil || got.NoncurrentVersionExpirationDays.IsUnknown() || got.NoncurrentVersionExpirationDays.ValueInt64() != 30 {
		t.Fatalf("state must hold the retention the platform answered: %+v", got)
	}
}

// Mineur 2 de la revue #113 : une durée absente de la configuration arrive dans
// le plan avec la valeur de l'état (UseStateForUnknown), peut-être périmée
// (plan sauvegardé, -refresh=false). Elle ne doit pas partir.
func TestUpdate_UnconfiguredRetention_IsNotSent(t *testing.T) {
	var bodies []map[string]any
	srv := testutil.NewTestServer(t, bucketRoutes(putCapture(&bodies, versioningBody("Suspended", 90, "applied"))))
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	plan := withVersioning(model(false, nullRules()), vers("Suspended", i64(30)))
	config := withVersioning(model(false, nullRules()), vers("Suspended", nil))
	resp := runUpdateWithConfig(t, srv, state, plan, config)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(bodies) != 1 || bodies[0]["status"] != "Suspended" {
		t.Fatalf("PUT bodies = %v", bodies)
	}
	if _, sent := bodies[0]["noncurrent_version_expiration_days"]; sent {
		t.Fatalf("a retention absent from the configuration must not be sent: %v", bodies[0])
	}
	if got := versioningOf(t, resp.State); got.NoncurrentVersionExpirationDays.ValueInt64() != 90 {
		t.Fatalf("state must hold the retention the platform answered (changed elsewhere): %+v", got)
	}
}

func TestUpdate_UnconfiguredRetention_NoStatusChange_NoCall(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes())
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	plan := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	config := withVersioning(model(false, nullRules()), vers("Enabled", nil))
	resp := runUpdateWithConfig(t, srv, state, plan, config)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	noVersioningCall(t, srv)
}

// Durée écrite dans la configuration : elle part, même inchangée, avec le statut.
func TestUpdate_Suspend_KeepsTheRetention(t *testing.T) {
	var bodies []map[string]any
	srv := testutil.NewTestServer(t, bucketRoutes(putCapture(&bodies, versioningBody("Suspended", 30, "applied"))))
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	plan := withVersioning(model(false, nullRules()), vers("Suspended", i64(30)))
	resp := runUpdate(t, srv, state, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(bodies) != 1 || bodies[0]["status"] != "Suspended" || bodies[0]["noncurrent_version_expiration_days"] != float64(30) {
		t.Fatalf("PUT bodies = %v", bodies)
	}
	if got := versioningOf(t, resp.State); got.Status.ValueString() != "Suspended" {
		t.Fatalf("state = %+v", got)
	}
}

func TestUpdate_RetentionChange_SendsIt(t *testing.T) {
	var bodies []map[string]any
	srv := testutil.NewTestServer(t, bucketRoutes(putCapture(&bodies, versioningBody("Suspended", 90, "applied"))))
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Suspended", i64(30)))
	plan := withVersioning(model(false, nullRules()), vers("Suspended", i64(90)))
	resp := runUpdate(t, srv, state, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(bodies) != 1 || bodies[0]["status"] != "Suspended" || bodies[0]["noncurrent_version_expiration_days"] != float64(90) {
		t.Fatalf("a retention change keeps the status and sends the days: %v", bodies)
	}
}

func TestUpdate_UnchangedVersioning_NoCall_StateKept(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes(
		testutil.Route{Method: "PATCH", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(true)},
	))
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(45)))
	plan := withVersioning(model(true, nullRules()), versUnknownDays("Enabled"))
	resp := runUpdate(t, srv, state, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	noVersioningCall(t, srv)
	got := versioningOf(t, resp.State)
	if got.NoncurrentVersionExpirationDays.IsUnknown() || got.NoncurrentVersionExpirationDays.ValueInt64() != 45 {
		t.Fatalf("an unwritten block must keep the known retention: %+v", got)
	}
}

func TestUpdate_RemovingTheBlock_StopsManaging_NeverDisables(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes())
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	resp := runUpdate(t, srv, state, model(false, nullRules()))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	noVersioningCall(t, srv)
	if versioningOf(t, resp.State) != nil {
		t.Fatal("state must no longer hold a managed versioning")
	}
}

func TestUpdate_Refused_ShowsThePlatformReason(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes(testutil.Route{Method: "PUT", Path: versioningPath, Status: 409,
		Body: map[string]any{"detail": "Une règle de cycle de vie garde les versions 365 jours : indiquez au moins 365 jours."}}))
	defer srv.Close()
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(400)))
	plan := withVersioning(model(false, nullRules()), vers("Enabled", i64(60)))
	resp := runUpdate(t, srv, state, plan)
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "au moins 365 jours") {
		t.Fatalf("the refusal must fail the apply with the platform's reason: %v", resp.Diagnostics)
	}
}

// ── Création ────────────────────────────────────────────────────────────────

func TestApplyVersioningOnCreate_Success(t *testing.T) {
	var bodies []map[string]any
	srv := testutil.NewTestServer(t, testutil.Routes{putCapture(&bodies, versioningBody("Enabled", 90, "applied"))})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	plan := withVersioning(model(false, nullRules()), vers("Enabled", i64(90)))
	if d := r.applyVersioningOnCreate(context.Background(), bucketID, &plan); d.HasError() || d.WarningsCount() != 0 {
		t.Fatal(d)
	}
	if len(bodies) != 1 || bodies[0]["noncurrent_version_expiration_days"] != float64(90) {
		t.Fatalf("PUT bodies = %v", bodies)
	}
}

func TestApplyVersioningOnCreate_WithoutBlock_NoCall(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	plan := model(false, nullRules())
	if d := r.applyVersioningOnCreate(context.Background(), bucketID, &plan); d.HasError() {
		t.Fatal(d)
	}
	noVersioningCall(t, srv)
}

// Un refus à la création ne doit pas rendre le bucket tainté : avertissement, et
// aucune valeur inconnue dans l'état.
func TestApplyVersioningOnCreate_RefusalDoesNotTaint(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PUT", Path: versioningPath, Status: 502,
		Body: map[string]any{"detail": "Versionnage enregistré mais non appliqué (stockage injoignable)."}}})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	plan := withVersioning(model(false, nullRules()), versUnknownDays("Enabled"))
	d := r.applyVersioningOnCreate(context.Background(), bucketID, &plan)
	if d.HasError() {
		t.Fatalf("must not be an error (it would taint the bucket): %v", d)
	}
	if d.WarningsCount() != 1 || !strings.Contains(d.Warnings()[0].Detail(), "non appliqué") {
		t.Fatalf("want one warning with the reason, got %v", d)
	}
	m, _ := versioningFromObject(context.Background(), plan.Versioning)
	if m == nil || m.NoncurrentVersionExpirationDays.IsUnknown() {
		t.Fatalf("the retention must be known after apply: %+v", m)
	}
}

func runCreate(t *testing.T, srv *testutil.Server, plan objectBucketResourceModel) resource.CreateResponse {
	t.Helper()
	ctx := context.Background()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sc resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sc)
	pl := tfsdk.State{Schema: sc.Schema}
	if d := pl.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: sc.Schema, Raw: tftypes.NewValue(sc.Schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sc.Schema}}, &resp)
	return resp
}

func createRoutes(extra ...testutil.Route) testutil.Routes {
	return append(testutil.Routes{
		{Method: "POST", Path: "/v1/buckets", Status: 201, Body: bucketBody(false)},
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(false)},
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(false)},
		credsRoute(),
	}, extra...)
}

func TestCreate_WithVersioningBlock_WritesIt(t *testing.T) {
	var bodies []map[string]any
	srv := testutil.NewTestServer(t, createRoutes(putCapture(&bodies, versioningBody("Enabled", 30, "applied"))))
	defer srv.Close()
	plan := withVersioning(model(false, nullRules()), versUnknownDays("Enabled"))
	plan.ID, plan.CorsRevision = types.StringUnknown(), types.StringUnknown()
	resp := runCreate(t, srv, plan)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if len(bodies) != 1 || bodies[0]["status"] != "Enabled" {
		t.Fatalf("the create must write the versioning: %v", paths(srv))
	}
	if got := versioningOf(t, resp.State); got == nil || got.NoncurrentVersionExpirationDays.ValueInt64() != 30 {
		t.Fatalf("state = %+v", got)
	}
}

func TestCreate_VersioningRefused_BucketKeptWithWarning(t *testing.T) {
	srv := testutil.NewTestServer(t, createRoutes(testutil.Route{Method: "PUT", Path: versioningPath, Status: 409,
		Body: map[string]any{"detail": "Une modification de ce bucket est déjà en cours."}}))
	defer srv.Close()
	plan := withVersioning(model(false, nullRules()), versUnknownDays("Enabled"))
	plan.ID, plan.CorsRevision = types.StringUnknown(), types.StringUnknown()
	resp := runCreate(t, srv, plan)
	if resp.Diagnostics.HasError() {
		t.Fatalf("a refused versioning must not fail the create (tainted bucket): %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("want one warning: %v", resp.Diagnostics)
	}
}

// ── Read ────────────────────────────────────────────────────────────────────

func runRead(t *testing.T, srv *testutil.Server, state objectBucketResourceModel) resource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sc resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sc)
	st := tfsdk.State{Schema: sc.Schema}
	if d := st.Set(ctx, &state); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.ReadResponse{State: st}
	r.Read(ctx, resource.ReadRequest{State: st}, &resp)
	return resp
}

func TestRead_ManagedVersioning_FollowsThePlatform(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes(testutil.Route{Method: "GET", Path: versioningPath, Status: 200,
		Body: versioningBody("Suspended", 60, "pending")}))
	defer srv.Close()
	resp := runRead(t, srv, withVersioning(model(false, nullRules()), vers("Enabled", i64(30))))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := versioningOf(t, resp.State)
	if got.Status.ValueString() != "Suspended" || got.NoncurrentVersionExpirationDays.ValueInt64() != 60 {
		t.Fatalf("a change made elsewhere must show as drift: %+v", got)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("a pending versioning must warn: %v", resp.Diagnostics)
	}
}

func TestRead_WithoutBlock_NoCall(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes())
	defer srv.Close()
	resp := runRead(t, srv, model(false, nullRules()))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	noVersioningCall(t, srv)
}

func TestVersioningStatusWarning(t *testing.T) {
	msg := "stockage indisponible"
	if _, detail, ok := versioningStatusWarning("b", &client.BucketVersioning{SyncStatus: "error", ErrorMessage: &msg}); !ok || !strings.Contains(detail, msg) {
		t.Fatalf("error status must warn with the message: %v %q", ok, detail)
	}
	if _, _, ok := versioningStatusWarning("b", &client.BucketVersioning{SyncStatus: "pending"}); !ok {
		t.Fatal("pending status must warn")
	}
	for _, s := range []string{"applied", "none", "unmanaged"} {
		if _, _, ok := versioningStatusWarning("b", &client.BucketVersioning{SyncStatus: s}); ok {
			t.Fatalf("status %q must not warn", s)
		}
	}
}

// ── Plan ────────────────────────────────────────────────────────────────────

func runModifyPlan(t *testing.T, state *objectBucketResourceModel, plan objectBucketResourceModel) resource.ModifyPlanResponse {
	t.Helper()
	return runModifyPlanWith(t, &objectBucketResource{}, state, plan)
}

func runModifyPlanWith(t *testing.T, r *objectBucketResource, state *objectBucketResourceModel, plan objectBucketResourceModel) resource.ModifyPlanResponse {
	t.Helper()
	ctx := context.Background()
	var sc resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sc)
	pl := tfsdk.State{Schema: sc.Schema}
	if d := pl.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	st := tfsdk.State{Schema: sc.Schema, Raw: tftypes.NewValue(sc.Schema.Type().TerraformType(ctx), nil)}
	if state != nil {
		if d := st.Set(ctx, state); d.HasError() {
			t.Fatal(d)
		}
	}
	req := resource.ModifyPlanRequest{Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sc.Schema}, State: st}
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	r.ModifyPlan(ctx, req, &resp)
	return resp
}

func TestModifyPlan_NewBucketCannotBeCreatedSuspended(t *testing.T) {
	resp := runModifyPlan(t, nil, withVersioning(model(false, nullRules()), versUnknownDays("Suspended")))
	if !resp.Diagnostics.HasError() {
		t.Fatal("creating a bucket with suspended versioning must be refused at plan time")
	}
	if ok := runModifyPlan(t, nil, withVersioning(model(false, nullRules()), versUnknownDays("Enabled"))); ok.Diagnostics.HasError() {
		t.Fatal(ok.Diagnostics)
	}
}

func TestModifyPlan_SuspendingAnExistingBucketIsAllowed(t *testing.T) {
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	resp := runModifyPlan(t, &state, withVersioning(model(false, nullRules()), vers("Suspended", i64(30))))
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 0 {
		t.Fatal(resp.Diagnostics)
	}
}

func TestModifyPlan_RemovingTheBlockWarnsItIsNotDisabled(t *testing.T) {
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	resp := runModifyPlan(t, &state, model(false, nullRules()))
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 ||
		!strings.Contains(resp.Diagnostics.Warnings()[0].Summary(), "does not disable") {
		t.Fatalf("want one warning: %v", resp.Diagnostics)
	}
}

func TestModifyPlan_ShorterRetentionWarns_LongerDoesNot(t *testing.T) {
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(365)))
	shorter := runModifyPlan(t, &state, withVersioning(model(false, nullRules()), vers("Enabled", i64(7))))
	if shorter.Diagnostics.WarningsCount() != 1 || !strings.Contains(shorter.Diagnostics.Warnings()[0].Detail(), "365 to 7") {
		t.Fatalf("a shorter retention must warn: %v", shorter.Diagnostics)
	}
	longer := runModifyPlan(t, &state, withVersioning(model(false, nullRules()), vers("Enabled", i64(400))))
	if longer.Diagnostics.WarningsCount() != 0 {
		t.Fatalf("a longer retention must not warn: %v", longer.Diagnostics)
	}
}

func TestModifyPlan_Destroy_NoCheck(t *testing.T) {
	ctx := context.Background()
	r := &objectBucketResource{}
	var sc resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sc)
	null := tftypes.NewValue(sc.Schema.Type().TerraformType(ctx), nil)
	resp := resource.ModifyPlanResponse{}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: tfsdk.Plan{Raw: null, Schema: sc.Schema}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
}

// ── Prise en charge d'un bucket existant (bloquant de la revue #113) ────────
// Bloc ajouté à un bucket existant ou bucket importé : l'état ne connaît pas le
// versionnage en place, le plan doit le lire.

func takeOverPlan(t *testing.T, routes testutil.Routes, plan objectBucketResourceModel) (resource.ModifyPlanResponse, *testutil.Server) {
	t.Helper()
	srv := testutil.NewTestServer(t, routes)
	t.Cleanup(srv.Close)
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	state := model(false, nullRules()) // bucket existant, sans bloc
	return runModifyPlanWith(t, r, &state, plan), srv
}

func versioningGet(body map[string]any) testutil.Route {
	return testutil.Route{Method: "GET", Path: versioningPath, Status: 200, Body: body}
}

func TestModifyPlan_TakeOverUnmanaged_WarnsWithTheRetentionSet(t *testing.T) {
	cases := map[string]struct {
		plan objectBucketResourceModel
		want string
	}{
		"default":  {withVersioning(model(false, nullRules()), versUnknownDays("Enabled")), "30 days"},
		"explicit": {withVersioning(model(false, nullRules()), vers("Suspended", i64(90))), "90 days"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resp, _ := takeOverPlan(t, testutil.Routes{versioningGet(versioningBody("Enabled", nil, "unmanaged"))}, c.plan)
			if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 {
				t.Fatalf("want one warning: %v", resp.Diagnostics)
			}
			w := resp.Diagnostics.Warnings()[0]
			if !strings.Contains(w.Summary(), "outside the platform") || !strings.Contains(w.Detail(), c.want) {
				t.Fatalf("the warning must say which retention is set: %q / %q", w.Summary(), w.Detail())
			}
		})
	}
}

func TestModifyPlan_ImportedManagedBucket_ShorterRetentionWarns(t *testing.T) {
	resp, _ := takeOverPlan(t, testutil.Routes{versioningGet(versioningBody("Enabled", 365, "applied"))},
		withVersioning(model(false, nullRules()), vers("Enabled", i64(7))))
	if resp.Diagnostics.WarningsCount() != 1 || !strings.Contains(resp.Diagnostics.Warnings()[0].Detail(), "365 to 7") {
		t.Fatalf("a shorter retention than the one in place must warn: %v", resp.Diagnostics)
	}
}

func TestModifyPlan_ImportedManagedBucket_SameOrUnsetRetention_NoWarning(t *testing.T) {
	for name, plan := range map[string]objectBucketResourceModel{
		"unset":  withVersioning(model(false, nullRules()), versUnknownDays("Enabled")),
		"longer": withVersioning(model(false, nullRules()), vers("Enabled", i64(400))),
	} {
		t.Run(name, func(t *testing.T) {
			resp, _ := takeOverPlan(t, testutil.Routes{versioningGet(versioningBody("Enabled", 365, "applied"))}, plan)
			if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 0 {
				t.Fatalf("no warning expected: %v", resp.Diagnostics)
			}
		})
	}
}

func TestModifyPlan_TakeOver_ReadFailureWarns(t *testing.T) {
	resp, _ := takeOverPlan(t, testutil.Routes{{Method: "GET", Path: versioningPath, Status: 500,
		Body: map[string]any{"detail": "indisponible"}}}, withVersioning(model(false, nullRules()), versUnknownDays("Enabled")))
	if resp.Diagnostics.HasError() || resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("a failed read must warn, not fail the plan: %v", resp.Diagnostics)
	}
}

func TestModifyPlan_StateWithBlock_DoesNotReadTheBucket(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	state := withVersioning(model(false, nullRules()), vers("Enabled", i64(30)))
	resp := runModifyPlanWith(t, r, &state, withVersioning(model(false, nullRules()), vers("Suspended", i64(30))))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	noVersioningCall(t, srv)
}
