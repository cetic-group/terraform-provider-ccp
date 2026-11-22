package appgwtargetgroup

import (
	"encoding/json"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestApplyToModelHealthCheckState(t *testing.T) {
	cases := []struct {
		name  string
		value *bool
		want  bool
	}{
		{"legacy response omits field", nil, true},
		{"health check enabled", boolPtr(true), true},
		{"health check disabled", boolPtr(false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &tgResourceModel{HCEnabled: types.BoolValue(!tc.want)}
			applyToModel(&client.AppGWTargetGroup{ID: "tg-1", AppGWID: "gw-1", HCEnabled: tc.value}, model)
			if model.HCEnabled.IsNull() || model.HCEnabled.ValueBool() != tc.want {
				t.Fatalf("hc_enabled = %v, want %v", model.HCEnabled, tc.want)
			}
		})
	}
}

func TestTargetGroupHealthCheckRequestJSON(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    string
	}{
		{"legacy create", client.AppGWTargetGroupCreateRequest{Name: "pool"}, `{"name":"pool"}`},
		{"disabled create", client.AppGWTargetGroupCreateRequest{Name: "pool", HCEnabled: boolPtr(false)}, `{"name":"pool","hc_enabled":false}`},
		{"disabled update", client.AppGWTargetGroupUpdateRequest{HCEnabled: boolPtr(false)}, `{"hc_enabled":false}`},
		{"enabled update", client.AppGWTargetGroupUpdateRequest{HCEnabled: boolPtr(true)}, `{"hc_enabled":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("JSON = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestHealthCheckConfirmation(t *testing.T) {
	cases := []struct {
		name          string
		actual        *bool
		expected      bool
		legacyDefault bool
		want          bool
	}{
		{"legacy default", nil, true, true, true},
		{"legacy cannot disable", nil, false, true, false},
		{"legacy cannot confirm update", nil, true, false, false},
		{"confirmed disabled", boolPtr(false), false, false, true},
		{"confirmed enabled", boolPtr(true), true, false, true},
		{"mismatched disabled", boolPtr(true), false, false, false},
		{"mismatched enabled", boolPtr(false), true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := healthCheckConfirmed(tc.actual, tc.expected, tc.legacyDefault); got != tc.want {
				t.Errorf("confirmation = %v, want %v", got, tc.want)
			}
		})
	}
}

func boolPtr(v bool) *bool { return &v }
