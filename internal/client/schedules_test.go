// Tests for the schedule validity period: a platform that predates the
// period fields must be detected, and a PATCH must keep "unchanged" (field
// omitted) distinct from "clear" (explicit null).
package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func scheduleBody(extra map[string]any) map[string]any {
	b := map[string]any{
		"id": "s-1", "name": "mondays", "resource_type": "vm", "resource_id": "vm-1",
		"timezone": "Europe/Paris", "enabled": true, "current_state": "on",
		"windows":                     []map[string]int{{"start_day": 0, "start_hour": 8, "end_day": 0, "end_hour": 20}},
		"estimated_monthly_fee_cents": 39,
	}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

func TestSchedule_SupportsPeriod(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want bool
	}{
		{"fields present, null", scheduleBody(map[string]any{"starts_on": nil, "ends_on": nil, "ended_at": nil}), true},
		{"fields present, set", scheduleBody(map[string]any{"starts_on": "2026-10-20", "ends_on": "2027-02-19"}), true},
		{"older platform", scheduleBody(nil), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.body)
			var s client.Schedule
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatal(err)
			}
			if s.SupportsPeriod != tc.want {
				t.Fatalf("SupportsPeriod = %v, want %v", s.SupportsPeriod, tc.want)
			}
			if s.Name != "mondays" || len(s.Windows) != 1 {
				t.Fatalf("regular fields lost by custom decoding: %+v", s)
			}
		})
	}
}

func TestSchedule_PeriodDecoded(t *testing.T) {
	raw, _ := json.Marshal(scheduleBody(map[string]any{
		"starts_on": "2026-10-20", "ends_on": "2027-02-19", "ended_at": "2027-02-20T00:01:00Z",
	}))
	var s client.Schedule
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.StartsOn == nil || *s.StartsOn != "2026-10-20" || s.EndsOn == nil || *s.EndsOn != "2027-02-19" ||
		s.EndedAt == nil {
		t.Fatalf("period not decoded: %+v", s)
	}
}

func TestScheduleUpdatePayload_Marshal(t *testing.T) {
	name := "renamed"
	cases := []struct {
		name string
		p    client.ScheduleUpdatePayload
		want string
	}{
		{"unchanged period omitted", client.ScheduleUpdatePayload{Name: &name}, `{"name":"renamed"}`},
		{"clear end sends null", client.ScheduleUpdatePayload{EndsOn: client.ClearDate()}, `{"ends_on":null}`},
		{"set start", client.ScheduleUpdatePayload{StartsOn: client.SetDate("2026-10-20")}, `{"starts_on":"2026-10-20"}`},
		{
			"both with name",
			client.ScheduleUpdatePayload{Name: &name, StartsOn: client.ClearDate(), EndsOn: client.SetDate("2027-02-19")},
			`{"ends_on":"2027-02-19","name":"renamed","starts_on":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.p)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCreateSchedule_PeriodOnlyWhenSet(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/schedules", Status: http.StatusCreated, Body: scheduleBody(nil)},
		{Method: "POST", Path: "/v1/schedules", Status: http.StatusCreated, Body: scheduleBody(nil)},
	})
	defer srv.Close()
	c := newClient(srv.URL)
	end := "2027-02-19"
	if _, err := c.CreateSchedule(context.Background(), client.ScheduleCreatePayload{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateSchedule(context.Background(), client.ScheduleCreatePayload{Name: "b", EndsOn: &end}); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls()
	var first, second map[string]any
	_ = json.Unmarshal(calls[0].Body, &first)
	_ = json.Unmarshal(calls[1].Body, &second)
	if _, ok := first["ends_on"]; ok {
		t.Fatalf("unbounded schedule must not send ends_on: %v", first)
	}
	if second["ends_on"] != end {
		t.Fatalf("ends_on not sent: %v", second)
	}
}
