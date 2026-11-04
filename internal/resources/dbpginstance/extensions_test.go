package dbpginstance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func catalog16() *client.DbPgExtensionCatalog {
	return &client.DbPgExtensionCatalog{
		Version: 16,
		Extensions: []client.DbPgExtension{
			{Name: "postgis"},
			{Name: "postgis_topology", Requires: []string{"postgis"}},
			{Name: "vector"},
			{Name: "pg_cron", RequiresPreload: true, SingleDatabase: true},
			{Name: "pg_stat_statements", RequiresPreload: true},
		},
		PreloadLibraries: []string{"pg_cron", "pg_stat_statements"},
		DirectExtensions: []string{"pgcrypto", "pg_trgm"},
	}
}

func TestCheckExtensionsAcceptsAValidConfiguration(t *testing.T) {
	got := checkExtensions(catalog16(),
		[]string{"pg_cron", "postgis", "postgis_topology"}, true,
		[]string{"pg_cron"}, true)
	if len(got) != 0 {
		t.Fatalf("unexpected problems: %+v", got)
	}
}

func TestCheckExtensionsReportsEachMistake(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		preload []string
		attr    string
		want    string
	}{
		{"dependency missing", []string{"postgis_topology"}, []string{}, "allowed_extensions", `add "postgis"`},
		{"needs no authorization", []string{"pgcrypto"}, []string{}, "allowed_extensions", "needs no authorization"},
		{"not in the catalog", []string{"plpython3u"}, []string{}, "allowed_extensions", "not in the extension catalog"},
		{"preload missing", []string{"pg_cron"}, []string{}, "preload_libraries", `add "pg_cron" to preload_libraries`},
		{"library not preloadable", []string{}, []string{"postgis"}, "preload_libraries", "cannot be preloaded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkExtensions(catalog16(), c.allowed, true, c.preload, true)
			if len(got) != 1 || got[0].attr != c.attr || !strings.Contains(got[0].msg, c.want) {
				t.Fatalf("got %+v, want one problem on %s containing %q", got, c.attr, c.want)
			}
		})
	}
}

func TestCheckExtensionsSkipsUnknownPreload(t *testing.T) {
	// preload_libraries not known yet: no preload requirement is checked.
	if got := checkExtensions(catalog16(), []string{"pg_cron"}, true, nil, false); len(got) != 0 {
		t.Fatalf("unexpected problems: %+v", got)
	}
}

func TestMajorOf(t *testing.T) {
	cases := map[types.String]int{
		types.StringNull():        16,
		types.StringUnknown():     16,
		types.StringValue("15"):   15,
		types.StringValue("16.4"): 16,
		types.StringValue("x"):    16,
	}
	for in, want := range cases {
		if got := majorOf(in); got != want {
			t.Errorf("majorOf(%v) = %d, want %d", in, got, want)
		}
	}
}

// setOf builds a known set; with no argument, an empty set (`[]` in HCL),
// never null.
func setOf(v ...string) types.Set {
	if v == nil {
		v = []string{}
	}
	s, _ := types.SetValueFrom(context.Background(), types.StringType, v)
	return s
}

func TestExtensionRequestAtCreationSendsOnlyNonEmptySettings(t *testing.T) {
	ctx := context.Background()
	plan := dbpgResourceModel{
		AllowedExtensions: setOf(), PreloadLibraries: types.SetUnknown(types.StringType),
		CronDatabase: types.StringUnknown(),
	}
	if _, send := extensionRequest(ctx, plan, nil); send {
		t.Fatal("an instance starts without extensions: nothing to send")
	}
	plan.AllowedExtensions = setOf("vector", "postgis")
	req, send := extensionRequest(ctx, plan, nil)
	if !send || req.AllowedExtensions == nil || strings.Join(*req.AllowedExtensions, ",") != "postgis,vector" {
		t.Fatalf("got %+v", req)
	}
	if req.PreloadLibraries != nil || req.CronDatabase != nil {
		t.Fatalf("unknown settings must not be sent: %+v", req)
	}
}

func TestExtensionRequestOnUpdateSendsOnlyChanges(t *testing.T) {
	ctx := context.Background()
	state := dbpgResourceModel{
		AllowedExtensions: setOf("postgis"), PreloadLibraries: setOf("pg_cron"),
		CronDatabase: types.StringValue("jobs"),
	}
	plan := state
	if _, send := extensionRequest(ctx, plan, &state); send {
		t.Fatal("no change: nothing to send")
	}
	plan.AllowedExtensions = setOf() // revoke everything
	req, send := extensionRequest(ctx, plan, &state)
	if !send || req.AllowedExtensions == nil || len(*req.AllowedExtensions) != 0 {
		t.Fatalf("an emptied list must be sent as []: %+v", req)
	}
	if req.PreloadLibraries != nil || req.CronDatabase != nil {
		t.Fatalf("unchanged settings must not be sent: %+v", req)
	}
	body, _ := json.Marshal(req)
	if string(body) != `{"allowed_extensions":[]}` {
		t.Fatalf("body = %s", body)
	}
}

func instanceBody(status string) map[string]any {
	return map[string]any{
		"id": "db-1", "name": "app", "region": "RNN", "engine": "pg", "tier": "dev",
		"plan": "small", "vpc_id": "vpc-1", "vnet_id": "vnet-1", "status": "active",
		"replicas": 1, "storage_gb": 20, "cpu_millicores": 500, "memory_mb": 2048, "tags": []string{},
		"extensions_available": true, "allowed_extensions": []string{"postgis"},
		"preload_libraries": []string{}, "extensions_status": status,
	}
}

func TestWaitExtensionsAppliedWaitsForApplied(t *testing.T) {
	old := extensionsPollInterval
	extensionsPollInterval = time.Millisecond
	defer func() { extensionsPollInterval = old }()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("pending")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("pending")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("applied")},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := waitExtensionsApplied(context.Background(), c, "db-1")
	if err != nil || got.ExtensionsStatus != "applied" || srv.PendingRoutes() != 0 {
		t.Fatalf("got %+v, err %v, pending %d", got, err, srv.PendingRoutes())
	}
}

func TestWaitExtensionsAppliedTimesOutWithTheReason(t *testing.T) {
	oldI, oldT := extensionsPollInterval, extensionsApplyTimeout
	extensionsPollInterval, extensionsApplyTimeout = time.Millisecond, 0
	defer func() { extensionsPollInterval, extensionsApplyTimeout = oldI, oldT }()
	body := instanceBody("pending")
	body["error_message"] = "La mise à jour des extensions n'a pas pu être appliquée"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	_, err := waitExtensionsApplied(context.Background(), c, "db-1")
	if err == nil || !strings.Contains(err.Error(), "n'a pas pu être appliquée") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateExtensionsSurfacesThePlatformRefusal(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusConflict, Body: map[string]any{
			"detail": "Supprimez d'abord ces extensions (DROP EXTENSION) avant de les retirer : app : postgis.",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	empty := []string{}
	_, err := c.UpdateDbPgExtensions(context.Background(), "db-1",
		client.DbPgExtensionsUpdateRequest{AllowedExtensions: &empty})
	if err == nil || !strings.Contains(err.Error(), "DROP EXTENSION") {
		t.Fatalf("err = %v", err)
	}
	if calls := srv.Calls(); len(calls) != 1 || string(calls[0].Body) != `{"allowed_extensions":[]}` {
		t.Fatalf("calls = %+v", calls)
	}
}

func testSchema(t *testing.T) (*dbpgResource, resource.SchemaResponse) {
	t.Helper()
	r := &dbpgResource{}
	var sr resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &sr)
	return r, sr
}

// newInstanceModel is the plan of a new instance, as Terraform sends it: the
// configured settings are known, the omitted and computed ones unknown.
func newInstanceModel(allowed types.Set) dbpgResourceModel {
	return dbpgResourceModel{
		ID: types.StringUnknown(), Name: types.StringValue("app"), Region: types.StringValue("RNN"),
		VpcID: types.StringValue("vpc-1"), VnetID: types.StringValue("vnet-1"),
		Plan: types.StringValue("small"), StorageGB: types.Int64Value(20),
		Replicas: types.Int64Value(1), Tier: types.StringUnknown(), EngineVersion: types.StringUnknown(),
		Status: types.StringUnknown(), EndpointVnetIP: types.StringUnknown(), EndpointPort: types.Int64Unknown(),
		AdminUsername: types.StringUnknown(), AdminDatabase: types.StringUnknown(),
		CPUMillicores: types.Int64Unknown(), MemoryMB: types.Int64Unknown(),
		Tags: types.ListUnknown(types.StringType), ExtensionsAvailable: types.BoolUnknown(),
		AllowedExtensions: allowed, PreloadLibraries: types.SetUnknown(types.StringType),
		CronDatabase: types.StringUnknown(), ExtensionsStatus: types.StringUnknown(),
	}
}

// configOf is the configuration behind a plan: an unknown value is an
// omitted (null) attribute, unless listed in `unknownInConfig` (a value that
// depends on another resource).
func configOf(m dbpgResourceModel, unknownInConfig ...string) dbpgResourceModel {
	keep := map[string]bool{}
	for _, a := range unknownInConfig {
		keep[a] = true
	}
	nullIfUnknown := func(name string, v types.Set) types.Set {
		if v.IsUnknown() && !keep[name] {
			return types.SetNull(types.StringType)
		}
		return v
	}
	c := m
	c.ID, c.Tier, c.Status, c.EndpointVnetIP = types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull()
	c.EndpointPort, c.CPUMillicores, c.MemoryMB = types.Int64Null(), types.Int64Null(), types.Int64Null()
	c.AdminUsername, c.AdminDatabase, c.ExtensionsStatus = types.StringNull(), types.StringNull(), types.StringNull()
	c.ExtensionsAvailable, c.Tags = types.BoolNull(), types.ListNull(types.StringType)
	if c.EngineVersion.IsUnknown() {
		c.EngineVersion = types.StringNull()
	}
	if c.CronDatabase.IsUnknown() {
		c.CronDatabase = types.StringNull()
	}
	c.AllowedExtensions = nullIfUnknown("allowed_extensions", c.AllowedExtensions)
	c.PreloadLibraries = nullIfUnknown("preload_libraries", c.PreloadLibraries)
	return c
}

// runCreate calls Create as Terraform does: the response state starts null.
func runCreate(t *testing.T, ctx context.Context, srv *testutil.Server, allowed types.Set) (*resource.CreateResponse, dbpgResourceModel) {
	t.Helper()
	r, sr := testSchema(t)
	r.client = client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	plan := tfsdk.Plan{Schema: sr.Schema}
	m := newInstanceModel(allowed)
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	resp := &resource.CreateResponse{State: tfsdk.State{
		Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	var got dbpgResourceModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return resp, got
}

// assertKnown fails if a state written after an apply holds an unknown value.
func assertKnown(t *testing.T, got dbpgResourceModel) {
	t.Helper()
	if got.AllowedExtensions.IsUnknown() || got.PreloadLibraries.IsUnknown() ||
		got.CronDatabase.IsUnknown() || got.ExtensionsStatus.IsUnknown() {
		t.Errorf("unknown value in state: %v %v %v %v", got.AllowedExtensions,
			got.PreloadLibraries, got.CronDatabase, got.ExtensionsStatus)
	}
}

// A new instance without the extensions image: GET reports
// extensions_available=false and no extension, the PATCH is refused (409).
func instanceWithoutExtensions(status string) map[string]any {
	b := instanceBody("applied")
	b["status"] = status
	b["extensions_available"] = false
	b["allowed_extensions"] = []string{}
	return b
}

// TestCreateFailsWhenExtensionsAreRefused pins #1809: the platform refused
// the extension settings of a new instance (409) and the apply succeeded,
// with the refused extensions in state. The creation must now fail, and the
// state must hold the instance with the settings the platform reports.
func TestCreateFailsWhenExtensionsAreRefused(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/db/pg", Status: http.StatusCreated, Body: instanceWithoutExtensions("provisioning")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceWithoutExtensions("active")},
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusConflict, Body: map[string]any{
			"detail": "Les extensions ne sont pas encore activables sur cette instance.",
		}},
	})
	defer srv.Close()

	resp, got := runCreate(t, context.Background(), srv, setOf("postgis"))

	if !resp.Diagnostics.HasError() {
		t.Fatalf("a refused extension setting must fail the creation, got %v", resp.Diagnostics)
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	for _, want := range []string{"cannot be enabled on this instance yet", "region where extensions are offered",
		"request their activation", "terraform untaint"} {
		if !strings.Contains(detail, want) {
			t.Errorf("message must contain %q: %s", want, detail)
		}
	}
	if strings.Contains(strings.ToLower(detail), "support") {
		t.Errorf("message must not send the user to support: %s", detail)
	}
	// The instance exists: it stays in state, or the next apply strands it.
	if got.ID.ValueString() != "db-1" {
		t.Fatalf("the created instance must be in state, id = %v", got.ID)
	}
	// Never the refused settings: the platform reports none.
	if !got.AllowedExtensions.Equal(setOf()) {
		t.Errorf("state must hold the platform's extensions, not the refused ones: %v", got.AllowedExtensions)
	}
	if got.ExtensionsAvailable.ValueBool() {
		t.Errorf("extensions_available = %v", got.ExtensionsAvailable)
	}
	assertKnown(t, got)
	if srv.PendingRoutes() != 0 {
		t.Errorf("%d fixtures not consumed", srv.PendingRoutes())
	}
}

// TestCreateReportsAnotherRefusalWithThePlatformMessage: on an instance where
// extensions are available, a refusal is the platform's own (here a race on
// the status), reported as such.
func TestCreateReportsAnotherRefusalWithThePlatformMessage(t *testing.T) {
	active := instanceWithoutExtensions("active")
	active["extensions_available"] = true
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/db/pg", Status: http.StatusCreated, Body: instanceWithoutExtensions("provisioning")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: active},
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusConflict, Body: map[string]any{
			"detail": "L'instance doit être active.",
		}},
	})
	defer srv.Close()

	resp, got := runCreate(t, context.Background(), srv, setOf("postgis"))

	if !resp.Diagnostics.HasError() ||
		!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "L'instance doit être active.") {
		t.Fatalf("got %v", resp.Diagnostics)
	}
	if !got.AllowedExtensions.Equal(setOf()) {
		t.Errorf("state must hold the platform's extensions: %v", got.AllowedExtensions)
	}
}

// TestCreateAppliesExtensions: accepted and in effect, the creation succeeds
// with the platform's settings.
func TestCreateAppliesExtensions(t *testing.T) {
	oldI := extensionsPollInterval
	extensionsPollInterval = time.Millisecond
	defer func() { extensionsPollInterval = oldI }()
	active := instanceWithoutExtensions("active")
	active["extensions_available"] = true
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/db/pg", Status: http.StatusCreated, Body: instanceWithoutExtensions("provisioning")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: active},
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusOK, Body: instanceBody("pending")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("applied")},
	})
	defer srv.Close()

	resp, got := runCreate(t, context.Background(), srv, setOf("postgis"))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error: %v", resp.Diagnostics)
	}
	if !got.AllowedExtensions.Equal(setOf("postgis")) || got.ExtensionsStatus.ValueString() != "applied" {
		t.Errorf("got %v %v", got.AllowedExtensions, got.ExtensionsStatus)
	}
	if srv.PendingRoutes() != 0 {
		t.Errorf("%d fixtures not consumed", srv.PendingRoutes())
	}
}

// fastWait makes the extension wait return at once.
func fastWait(t *testing.T, timeout time.Duration) {
	oldI, oldT := extensionsPollInterval, extensionsApplyTimeout
	extensionsPollInterval, extensionsApplyTimeout = time.Millisecond, timeout
	t.Cleanup(func() { extensionsPollInterval, extensionsApplyTimeout = oldI, oldT })
}

func activeWithExtensions() map[string]any {
	b := instanceWithoutExtensions("active")
	b["extensions_available"] = true
	return b
}

// TestCreateFailsWhenExtensionsAreStillPending: accepted but not in effect
// when the wait ends. The state holds what the platform reports — the
// accepted settings, with extensions_status "pending" — and the message says
// the next apply waits for them.
func TestCreateFailsWhenExtensionsAreStillPending(t *testing.T) {
	fastWait(t, 0)
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/db/pg", Status: http.StatusCreated, Body: instanceWithoutExtensions("provisioning")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: activeWithExtensions()},
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusOK, Body: instanceBody("pending")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("pending")},
	})
	defer srv.Close()

	resp, got := runCreate(t, context.Background(), srv, setOf("postgis"))

	if !resp.Diagnostics.HasError() {
		t.Fatalf("settings not in effect must fail the creation, got %v", resp.Diagnostics)
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	for _, want := range []string{"not in effect yet", "terraform untaint", "waits for the settings"} {
		if !strings.Contains(detail, want) {
			t.Errorf("message must contain %q: %s", want, detail)
		}
	}
	if got.ID.ValueString() != "db-1" || got.ExtensionsStatus.ValueString() != extensionsPending {
		t.Errorf("state must hold the instance as reported, pending: %v %v", got.ID, got.ExtensionsStatus)
	}
	assertKnown(t, got)
	if srv.PendingRoutes() != 0 {
		t.Errorf("%d fixtures not consumed", srv.PendingRoutes())
	}
}

// TestCreateStopsWhenCancelled: interrupted while waiting, the creation
// fails, and the state holds the instance read before the PATCH.
func TestCreateStopsWhenCancelled(t *testing.T) {
	fastWait(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/db/pg", Status: http.StatusCreated, Body: instanceWithoutExtensions("provisioning")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: activeWithExtensions()},
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusOK, BodyFn: func(*testing.T, []byte) (int, any) {
			return http.StatusOK, instanceBody("pending")
		}},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, BodyFn: func(*testing.T, []byte) (int, any) {
			cancel()
			return http.StatusOK, instanceBody("pending")
		}},
	})
	defer srv.Close()

	resp, got := runCreate(t, ctx, srv, setOf("postgis"))

	if !resp.Diagnostics.HasError() {
		t.Fatalf("an interrupted wait must fail the creation, got %v", resp.Diagnostics)
	}
	if got.ID.ValueString() != "db-1" {
		t.Fatalf("the created instance must be in state, id = %v", got.ID)
	}
	assertKnown(t, got)
}

// TestCreateReportsOtherErrorsWithThePlatformMessage: on an instance without
// extensions, a refusal other than 409 is not mistaken for "not available".
func TestCreateReportsOtherErrorsWithThePlatformMessage(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/db/pg", Status: http.StatusCreated, Body: instanceWithoutExtensions("provisioning")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceWithoutExtensions("active")},
		{Method: "PATCH", Path: "/v1/db/pg/db-1/extensions", Status: http.StatusUnprocessableEntity, Body: map[string]any{
			"detail": "Extension(s) hors catalogue : plpython3u.",
		}},
	})
	defer srv.Close()

	resp, got := runCreate(t, context.Background(), srv, setOf("postgis"))

	if !resp.Diagnostics.HasError() {
		t.Fatalf("got %v", resp.Diagnostics)
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "hors catalogue") || strings.Contains(detail, "cannot be enabled on this instance yet") {
		t.Errorf("the platform's own refusal must be reported: %s", detail)
	}
	if !got.AllowedExtensions.Equal(setOf()) {
		t.Errorf("state must hold the platform's extensions: %v", got.AllowedExtensions)
	}
}

// existingState is the state of an active instance with postgis allowed.
func existingState(status string) dbpgResourceModel {
	m := newInstanceModel(setOf("postgis"))
	m.ID, m.Tier, m.EngineVersion, m.Status = types.StringValue("db-1"), types.StringValue("dev"), types.StringValue("16"), types.StringValue("active")
	m.EndpointVnetIP, m.EndpointPort = types.StringNull(), types.Int64Value(5432)
	m.AdminUsername, m.AdminDatabase = types.StringNull(), types.StringNull()
	m.CPUMillicores, m.MemoryMB = types.Int64Value(500), types.Int64Value(2048)
	m.Tags = types.ListValueMust(types.StringType, nil)
	m.ExtensionsAvailable = types.BoolValue(true)
	m.PreloadLibraries, m.CronDatabase = setOf(), types.StringNull()
	m.ExtensionsStatus = types.StringValue(status)
	return m
}

// runModifyPlan calls ModifyPlan with a plan, its configuration and a state
// (nil at creation), and returns the diagnostics and the modified plan.
func runModifyPlan(t *testing.T, srv *testutil.Server, plan, config dbpgResourceModel, state *dbpgResourceModel) (*resource.ModifyPlanResponse, dbpgResourceModel) {
	t.Helper()
	ctx := context.Background()
	r, sr := testSchema(t)
	url := "http://127.0.0.1:1"
	if srv != nil {
		url = srv.URL
	}
	r.client = client.New(url, "ccp_test_unit", "0.0.0-test")
	p := tfsdk.Plan{Schema: sr.Schema}
	c := tfsdk.Config{Schema: sr.Schema}
	st := tfsdk.State{Schema: sr.Schema, Raw: tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)}
	for _, d := range []interface{ HasError() bool }{p.Set(ctx, &plan), st.Set(ctx, state)} {
		if d.HasError() {
			t.Fatalf("set: %v", d)
		}
	}
	if state == nil {
		st.Raw = tftypes.NewValue(sr.Schema.Type().TerraformType(ctx), nil)
	}
	cs := tfsdk.State{Schema: sr.Schema}
	if d := cs.Set(ctx, &config); d.HasError() {
		t.Fatalf("config: %v", d)
	}
	c.Raw = cs.Raw
	resp := &resource.ModifyPlanResponse{Plan: p}
	r.ModifyPlan(ctx, resource.ModifyPlanRequest{Plan: p, Config: c, State: st}, resp)
	var got dbpgResourceModel
	if d := resp.Plan.Get(ctx, &got); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	return resp, got
}

// TestModifyPlanWaitsForPendingSettings: settings accepted by an earlier
// apply are still pending. A configuration equal to the state must still
// plan a change (extensions_status unknown), so that the apply waits.
func TestModifyPlanWaitsForPendingSettings(t *testing.T) {
	state := existingState(extensionsPending)
	plan := state
	_, got := runModifyPlan(t, nil, plan, configOf(plan), &state)
	if !got.ExtensionsStatus.IsUnknown() {
		t.Fatalf("pending settings must be planned for, extensions_status = %v", got.ExtensionsStatus)
	}

	applied := existingState("applied")
	_, got = runModifyPlan(t, nil, applied, configOf(applied), &applied)
	if got.ExtensionsStatus.ValueString() != "applied" {
		t.Fatalf("settings in effect plan nothing, extensions_status = %v", got.ExtensionsStatus)
	}
}

// TestModifyPlanChecksOmittedSettingsAtCreation: an omitted setting is
// unknown in the plan of a new instance, but the instance starts without it.
// The checks must treat it as empty instead of skipping them.
func TestModifyPlanChecksOmittedSettingsAtCreation(t *testing.T) {
	t.Run("cron_database without preload_libraries", func(t *testing.T) {
		plan := newInstanceModel(types.SetUnknown(types.StringType))
		plan.CronDatabase = types.StringValue("app")
		resp, _ := runModifyPlan(t, nil, plan, configOf(plan), nil)
		if !resp.Diagnostics.HasError() ||
			!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "cron_database only applies") {
			t.Fatalf("got %v", resp.Diagnostics)
		}
	})
	t.Run("pg_cron without preload_libraries", func(t *testing.T) {
		srv := testutil.NewTestServer(t, testutil.Routes{
			{Method: "GET", Path: "/v1/db/pg/extensions", Status: http.StatusOK, Body: catalog16()},
		})
		defer srv.Close()
		plan := newInstanceModel(setOf("pg_cron"))
		resp, _ := runModifyPlan(t, srv, plan, configOf(plan), nil)
		if !resp.Diagnostics.HasError() ||
			!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), `add "pg_cron" to preload_libraries`) {
			t.Fatalf("got %v", resp.Diagnostics)
		}
	})
	t.Run("value known only at apply is not checked", func(t *testing.T) {
		srv := testutil.NewTestServer(t, testutil.Routes{
			{Method: "GET", Path: "/v1/db/pg/extensions", Status: http.StatusOK, Body: catalog16()},
		})
		defer srv.Close()
		plan := newInstanceModel(setOf("pg_cron"))
		resp, _ := runModifyPlan(t, srv, plan, configOf(plan, "preload_libraries"), nil)
		if resp.Diagnostics.HasError() {
			t.Fatalf("a value not known yet cannot be checked: %v", resp.Diagnostics)
		}
	})
	t.Run("invalid cron_database name", func(t *testing.T) {
		plan := newInstanceModel(types.SetUnknown(types.StringType))
		plan.PreloadLibraries = setOf("pg_cron")
		plan.CronDatabase = types.StringValue("App-DB")
		srv := testutil.NewTestServer(t, testutil.Routes{
			{Method: "GET", Path: "/v1/db/pg/extensions", Status: http.StatusOK, Body: catalog16()},
		})
		defer srv.Close()
		resp, _ := runModifyPlan(t, srv, plan, configOf(plan), nil)
		if !resp.Diagnostics.HasError() ||
			!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "database name") {
			t.Fatalf("got %v", resp.Diagnostics)
		}
	})
}

// runUpdate calls Update as Terraform does: the response state starts as
// the prior state.
func runUpdate(t *testing.T, srv *testutil.Server, plan, state dbpgResourceModel) (*resource.UpdateResponse, dbpgResourceModel) {
	t.Helper()
	ctx := context.Background()
	r, sr := testSchema(t)
	r.client = client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	p := tfsdk.Plan{Schema: sr.Schema}
	st := tfsdk.State{Schema: sr.Schema}
	if d := p.Set(ctx, &plan); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	if d := st.Set(ctx, &state); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sr.Schema, Raw: st.Raw.Copy()}}
	r.Update(ctx, resource.UpdateRequest{Plan: p, State: st}, resp)
	var got dbpgResourceModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return resp, got
}

// TestUpdateWaitsForPendingSettings: nothing to send, but settings accepted
// earlier are pending. The apply waits until they are in effect.
func TestUpdateWaitsForPendingSettings(t *testing.T) {
	fastWait(t, time.Hour)
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("pending")},
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("applied")},
	})
	defer srv.Close()
	state := existingState(extensionsPending)
	plan := state
	plan.ExtensionsStatus = types.StringUnknown()

	resp, got := runUpdate(t, srv, plan, state)

	if resp.Diagnostics.HasError() || got.ExtensionsStatus.ValueString() != "applied" {
		t.Fatalf("got %v, %v", resp.Diagnostics, got.ExtensionsStatus)
	}
	if srv.PendingRoutes() != 0 {
		t.Errorf("the apply must wait until applied: %d fixtures not consumed", srv.PendingRoutes())
	}
}

// TestUpdateFailsWhilePendingSettingsAreNotInEffect: the wait ends first.
// The apply fails and keeps the prior state, still pending, so the next plan
// waits again.
func TestUpdateFailsWhilePendingSettingsAreNotInEffect(t *testing.T) {
	fastWait(t, 0)
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/db/pg/db-1", Status: http.StatusOK, Body: instanceBody("pending")},
	})
	defer srv.Close()
	state := existingState(extensionsPending)
	plan := state
	plan.ExtensionsStatus = types.StringUnknown()

	resp, got := runUpdate(t, srv, plan, state)

	if !resp.Diagnostics.HasError() ||
		!strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "waits for them again") {
		t.Fatalf("got %v", resp.Diagnostics)
	}
	if got.ExtensionsStatus.ValueString() != extensionsPending {
		t.Errorf("prior state must be kept: %v", got.ExtensionsStatus)
	}
}
