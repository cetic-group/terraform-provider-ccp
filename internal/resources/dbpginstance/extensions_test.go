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

// createPlan is the plan of a new instance, as Terraform sends it: the
// configured settings are known, the computed ones unknown.
func createPlan(t *testing.T, r *dbpgResource, allowed types.Set) tfsdk.Plan {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sr)
	m := dbpgResourceModel{
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
	plan := tfsdk.Plan{Schema: sr.Schema}
	if d := plan.Set(ctx, &m); d.HasError() {
		t.Fatalf("plan: %v", d)
	}
	return plan
}

// runCreate calls Create as Terraform does and returns its response.
func runCreate(t *testing.T, srv *testutil.Server, allowed types.Set) (*resource.CreateResponse, dbpgResourceModel) {
	t.Helper()
	ctx := context.Background()
	r := &dbpgResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	plan := createPlan(t, r, allowed)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema, Raw: plan.Raw.Copy()}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, resp)
	var got dbpgResourceModel
	if d := resp.State.Get(ctx, &got); d.HasError() {
		t.Fatalf("state: %v", d)
	}
	return resp, got
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

	resp, got := runCreate(t, srv, setOf("postgis"))

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
	// A state written after an apply holds no unknown value.
	if got.PreloadLibraries.IsUnknown() || got.CronDatabase.IsUnknown() || got.ExtensionsStatus.IsUnknown() {
		t.Errorf("unknown value in state: %v %v %v", got.PreloadLibraries, got.CronDatabase, got.ExtensionsStatus)
	}
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

	resp, got := runCreate(t, srv, setOf("postgis"))

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

	resp, got := runCreate(t, srv, setOf("postgis"))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error: %v", resp.Diagnostics)
	}
	if !got.AllowedExtensions.Equal(setOf("postgis")) || got.ExtensionsStatus.ValueString() != "applied" {
		t.Errorf("got %v %v", got.AllowedExtensions, got.ExtensionsStatus)
	}
}
