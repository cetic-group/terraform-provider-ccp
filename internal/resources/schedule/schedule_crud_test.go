// Create / Update of ccp_schedule run against a recording test server: the
// period must be sent, read back, and refused when the platform ignores it.
package schedule

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
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const sid = "s-1"

func schemaOf(t *testing.T) (resource.SchemaResponse, tftypes.Object) {
	t.Helper()
	var sresp resource.SchemaResponse
	(&scheduleResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sresp)
	return sresp, sresp.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
}

// value builds a full object: every attribute null unless overridden.
func value(objType tftypes.Object, over map[string]any) tftypes.Value {
	win := objType.AttributeTypes["windows"].(tftypes.List).ElementType.(tftypes.Object)
	fields := map[string]tftypes.Value{}
	for name, typ := range objType.AttributeTypes {
		fields[name] = tftypes.NewValue(typ, nil)
	}
	base := map[string]any{
		"name": "mondays", "resource_type": "vm", "resource_id": "vm-1",
		"timezone": "Europe/Paris", "enabled": true,
	}
	for k, v := range over {
		base[k] = v
	}
	for k, v := range base {
		if v == nil {
			continue
		}
		fields[k] = tftypes.NewValue(objType.AttributeTypes[k], v)
	}
	fields["windows"] = tftypes.NewValue(objType.AttributeTypes["windows"], []tftypes.Value{
		tftypes.NewValue(win, map[string]tftypes.Value{
			"start_day": tftypes.NewValue(tftypes.Number, 0), "start_hour": tftypes.NewValue(tftypes.Number, 8),
			"end_day": tftypes.NewValue(tftypes.Number, 0), "end_hour": tftypes.NewValue(tftypes.Number, 20),
		}),
	})
	return tftypes.NewValue(objType, fields)
}

func apiBody(extra map[string]any) map[string]any {
	b := map[string]any{
		"id": sid, "name": "mondays", "resource_type": "vm", "resource_id": "vm-1",
		"timezone": "Europe/Paris", "enabled": true, "current_state": "on",
		"windows":                     []map[string]int{{"start_day": 0, "start_hour": 8, "end_day": 0, "end_hour": 20}},
		"estimated_monthly_fee_cents": 39, "starts_on": nil, "ends_on": nil, "ended_at": nil,
	}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// oldPlatform drops the period fields, as a platform that predates them.
func oldPlatform(b map[string]any) map[string]any {
	delete(b, "starts_on")
	delete(b, "ends_on")
	delete(b, "ended_at")
	return b
}

func newResource(url string) *scheduleResource {
	return &scheduleResource{client: client.New(url, "ccp_test_unit", "0.0.0-test")}
}

func lastBody(t *testing.T, srv *testutil.Server, method string) map[string]any {
	t.Helper()
	calls := srv.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			var m map[string]any
			_ = json.Unmarshal(calls[i].Body, &m)
			return m
		}
	}
	t.Fatalf("no %s call", method)
	return nil
}

func doCreate(t *testing.T, r *scheduleResource, plan map[string]any) (*resource.CreateResponse, scheduleResourceModel) {
	sresp, objType := schemaOf(t)
	resp := &resource.CreateResponse{State: tfsdk.State{Schema: sresp.Schema}}
	r.Create(context.Background(), resource.CreateRequest{
		Plan: tfsdk.Plan{Raw: value(objType, plan), Schema: sresp.Schema},
	}, resp)
	var m scheduleResourceModel
	if !resp.Diagnostics.HasError() {
		resp.State.Get(context.Background(), &m)
	}
	return resp, m
}

func doUpdate(t *testing.T, r *scheduleResource, state, plan map[string]any) (*resource.UpdateResponse, scheduleResourceModel) {
	sresp, objType := schemaOf(t)
	state["id"], plan["id"] = sid, sid
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: sresp.Schema}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: value(objType, plan), Schema: sresp.Schema},
		State: tfsdk.State{Raw: value(objType, state), Schema: sresp.Schema},
	}, resp)
	var m scheduleResourceModel
	if !resp.Diagnostics.HasError() {
		resp.State.Get(context.Background(), &m)
	}
	return resp, m
}

func TestCreate_SendsAndReadsBackPeriod(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "POST", Path: "/v1/schedules", Status: http.StatusCreated,
		Body: apiBody(map[string]any{"starts_on": "2026-10-20", "ends_on": "2027-02-19"})}})
	defer srv.Close()
	resp, m := doCreate(t, newResource(srv.URL), map[string]any{"starts_on": "2026-10-20", "ends_on": "2027-02-19"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	b := lastBody(t, srv, "POST")
	if b["starts_on"] != "2026-10-20" || b["ends_on"] != "2027-02-19" {
		t.Fatalf("period not sent: %v", b)
	}
	if m.EndsOn.ValueString() != "2027-02-19" || !m.EndedAt.IsNull() {
		t.Fatalf("period not read back: %+v", m)
	}
}

func TestCreate_OldPlatform_RemovesScheduleAndFails(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/schedules", Status: http.StatusCreated, Body: oldPlatform(apiBody(nil))},
		{Method: "DELETE", Path: "/v1/schedules/" + sid, Status: http.StatusNoContent},
	})
	defer srv.Close()
	resp, _ := doCreate(t, newResource(srv.URL), map[string]any{"ends_on": "2027-02-19"})
	if !resp.Diagnostics.HasError() || !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "not created") {
		t.Fatalf("expected a refusal, got %v", resp.Diagnostics)
	}
	if srv.PendingRoutes() != 0 {
		t.Fatal("the schedule created without its period was not deleted")
	}
}

func TestCreate_OldPlatform_WithoutPeriodStillWorks(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/schedules", Status: http.StatusCreated, Body: oldPlatform(apiBody(nil))},
	})
	defer srv.Close()
	if resp, _ := doCreate(t, newResource(srv.URL), nil); resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
}

func TestUpdate_MovingEndRunsEndedScheduleAgain(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(map[string]any{"ends_on": "2027-03-31"})}})
	defer srv.Close()
	state := map[string]any{"ends_on": "2027-02-19", "ended_at": "2027-02-20T00:01:00Z"}
	resp, m := doUpdate(t, newResource(srv.URL), state, map[string]any{"ends_on": "2027-03-31"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	b := lastBody(t, srv, "PATCH")
	if b["ends_on"] != "2027-03-31" || b["enabled"] != true {
		t.Fatalf("expected ends_on + enabled=true, got %v", b)
	}
	if !m.EndedAt.IsNull() || m.EndsOn.ValueString() != "2027-03-31" {
		t.Fatalf("state not read back: %+v", m)
	}
}

// A plan made before the end and applied after it: ended_at is still null in
// state, but the platform has paused the schedule meanwhile.
func TestUpdate_PeriodChangeAlwaysSendsEnabledWhenConfigured(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(map[string]any{"ends_on": "2027-03-31"})}})
	defer srv.Close()
	resp, m := doUpdate(t, newResource(srv.URL), map[string]any{"ends_on": "2027-02-19"}, map[string]any{"ends_on": "2027-03-31"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if lastBody(t, srv, "PATCH")["enabled"] != true || !m.Enabled.ValueBool() {
		t.Fatal("enabled=true must be sent with a period change")
	}
}

func TestUpdate_PeriodChangeKeepsDisabledWhenConfigured(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(map[string]any{"enabled": false, "ends_on": "2027-03-31"})}})
	defer srv.Close()
	state := map[string]any{"enabled": false, "ends_on": "2027-02-19"}
	resp, _ := doUpdate(t, newResource(srv.URL), state, map[string]any{"enabled": false, "ends_on": "2027-03-31"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if _, sent := lastBody(t, srv, "PATCH")["enabled"]; sent {
		t.Fatal("a schedule configured disabled must not be enabled by a period change")
	}
}

func TestUpdate_RemovingEndSendsNull(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(nil)}})
	defer srv.Close()
	resp, m := doUpdate(t, newResource(srv.URL), map[string]any{"ends_on": "2027-02-19"}, map[string]any{})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	b := lastBody(t, srv, "PATCH")
	if v, ok := b["ends_on"]; !ok || v != nil {
		t.Fatalf("expected ends_on=null, got %v", b)
	}
	if !m.EndsOn.IsNull() {
		t.Fatal("ends_on must be null in state")
	}
}

// Import of an ended schedule (state enabled=false), configuration enabled=true:
// nothing is sent (it would be refused), the configured value is kept.
func TestUpdate_ReEnablingEndedScheduleConverges(t *testing.T) {
	ended := map[string]any{"enabled": false, "ends_on": "2027-02-19", "ended_at": "2027-02-20T00:01:00Z"}
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "GET", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(ended)}})
	defer srv.Close()
	state := map[string]any{"enabled": false, "ends_on": "2027-02-19", "ended_at": "2027-02-20T00:01:00Z"}
	resp, m := doUpdate(t, newResource(srv.URL), state, map[string]any{"ends_on": "2027-02-19"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, c := range srv.Calls() {
		if c.Method == "PATCH" {
			t.Fatalf("no PATCH expected, got %s", c.Body)
		}
	}
	if !m.Enabled.ValueBool() || m.EndedAt.IsNull() {
		t.Fatalf("state must keep enabled=true and ended_at: %+v", m)
	}
}

func TestUpdate_DisablingActiveScheduleIsSent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(map[string]any{"enabled": false})}})
	defer srv.Close()
	resp, m := doUpdate(t, newResource(srv.URL), map[string]any{}, map[string]any{"enabled": false})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if lastBody(t, srv, "PATCH")["enabled"] != false || m.Enabled.ValueBool() {
		t.Fatal("enabled=false must be sent")
	}
}

func TestUpdate_OldPlatformFails(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: oldPlatform(apiBody(nil))}})
	defer srv.Close()
	resp, _ := doUpdate(t, newResource(srv.URL), map[string]any{}, map[string]any{"ends_on": "2027-02-19"})
	if !resp.Diagnostics.HasError() {
		t.Fatal("a period ignored by the platform must fail the update")
	}
}

// A bound changed outside Terraform (console, CLI) must show up as drift.
func TestRead_PicksUpPeriodChangedElsewhere(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "GET", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(map[string]any{"starts_on": "2026-11-01", "ends_on": "2027-03-31"})}})
	defer srv.Close()
	sresp, objType := schemaOf(t)
	resp := &resource.ReadResponse{State: tfsdk.State{Schema: sresp.Schema}}
	newResource(srv.URL).Read(context.Background(), resource.ReadRequest{State: tfsdk.State{
		Raw: value(objType, map[string]any{"id": sid, "starts_on": "2026-10-20", "ends_on": "2027-02-19"}), Schema: sresp.Schema,
	}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var m scheduleResourceModel
	resp.State.Get(context.Background(), &m)
	if m.StartsOn.ValueString() != "2026-11-01" || m.EndsOn.ValueString() != "2027-03-31" {
		t.Fatalf("period changed elsewhere not read: %+v", m)
	}
}

func TestUpdate_StartsOnChangeIsSent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{{Method: "PATCH", Path: "/v1/schedules/" + sid, Status: http.StatusOK,
		Body: apiBody(map[string]any{"starts_on": "2026-11-02"})}})
	defer srv.Close()
	resp, m := doUpdate(t, newResource(srv.URL), map[string]any{"starts_on": "2026-10-20"}, map[string]any{"starts_on": "2026-11-02"})
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	if lastBody(t, srv, "PATCH")["starts_on"] != "2026-11-02" || m.StartsOn.ValueString() != "2026-11-02" {
		t.Fatal("starts_on change not sent")
	}
}
