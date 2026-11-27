package k8scluster

// `os_image` — default OS of new node pools (cetic-cloud-platform#2155):
// changed in place by the PATCH, `rocky9` refused on write and still read.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func osImageAttr(t *testing.T) schema.StringAttribute {
	t.Helper()
	resp := &resource.SchemaResponse{}
	New().Schema(context.Background(), resource.SchemaRequest{}, resp)
	return resp.Schema.Attributes["os_image"].(schema.StringAttribute)
}

// `rocky9` is withdrawn: refused in a configuration, every other family
// accepted.
func TestOsImage_Rocky9RefusedOnWrite(t *testing.T) {
	ctx := context.Background()
	a := osImageAttr(t)
	valid := func(v string) bool {
		for _, val := range a.Validators {
			r := &validator.StringResponse{}
			val.ValidateString(ctx, validator.StringRequest{Path: path.Root("os_image"), ConfigValue: types.StringValue(v)}, r)
			if r.Diagnostics.HasError() {
				return false
			}
		}
		return true
	}
	for _, v := range []string{"flatcar", "ubuntu", "rocky10"} {
		if !valid(v) {
			t.Errorf("%s must be accepted", v)
		}
	}
	for _, v := range []string{"rocky9", "debian"} {
		if valid(v) {
			t.Errorf("%s must be refused", v)
		}
	}
}

// No replacement any more: a change is planned in place. UseStateForUnknown
// stays, so an unset os_image keeps the state value (a `rocky9` one too).
func TestOsImage_InPlace(t *testing.T) {
	ctx := context.Background()
	a := osImageAttr(t)
	if len(a.PlanModifiers) != 1 {
		t.Fatalf("os_image plan modifiers = %d, want UseStateForUnknown only", len(a.PlanModifiers))
	}
	for _, tc := range []struct{ state, config, wantPlan string }{
		{"flatcar", "ubuntu", "ubuntu"},
		{"rocky9", "", "rocky9"},
	} {
		cfg := types.StringNull()
		if tc.config != "" {
			cfg = types.StringValue(tc.config)
		}
		planned := cfg
		if cfg.IsNull() {
			planned = types.StringUnknown() // Computed, unset
		}
		req := planmodifier.StringRequest{
			Path: path.Root("os_image"), ConfigValue: cfg, PlanValue: planned,
			StateValue: types.StringValue(tc.state),
			State:      tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})},
			Plan:       tfsdk.Plan{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})},
		}
		resp := &planmodifier.StringResponse{PlanValue: planned}
		for _, m := range a.PlanModifiers {
			m.PlanModifyString(ctx, req, resp)
		}
		if resp.RequiresReplace {
			t.Errorf("%s → %q: replacement planned", tc.state, tc.config)
		}
		if resp.PlanValue.ValueString() != tc.wantPlan {
			t.Errorf("%s → %q: plan = %s, want %s", tc.state, tc.config, resp.PlanValue, tc.wantPlan)
		}
	}
}

// A state (or an import) holding `rocky9` reads without error: only the
// configuration is validated.
func TestOsImage_Rocky9StateReads(t *testing.T) {
	ctx := context.Background()
	body := activeV2143("standard")
	body["os_image"] = "rocky9"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()
	got, err := client.New(srv.URL, "k", "test").GetK8sCluster(ctx, "cl-1")
	if err != nil {
		t.Fatal(err)
	}
	m, ds := stateFromAPI(ctx, got, nil)
	if len(ds) > 0 || m.OsImage.ValueString() != "rocky9" {
		t.Fatalf("os_image = %s (%v), want rocky9 read as is", m.OsImage, ds)
	}
}

// The Update PATCHes `os_image` only when it changes (and waits for an
// active cluster first, like a plan change).
func TestUpdate_OsImageByPatch(t *testing.T) {
	if v := defaultOSChange(types.StringValue("ubuntu"), types.StringValue("flatcar")); v == nil || *v != "ubuntu" {
		t.Fatalf("changed: %v", v)
	}
	if v := defaultOSChange(types.StringValue("rocky9"), types.StringValue("rocky9")); v != nil {
		t.Fatal("unchanged (rocky9 kept from the state): nothing sent")
	}
	if v := defaultOSChange(types.StringUnknown(), types.StringValue("flatcar")); v != nil {
		t.Fatal("unknown: nothing sent")
	}
	v := "rocky10"
	raw, _ := json.Marshal(client.K8sClusterUpdateRequest{OsImage: &v})
	if string(raw) != `{"os_image":"rocky10"}` {
		t.Fatalf("PATCH body = %s", raw)
	}
	raw, _ = json.Marshal(client.K8sClusterUpdateRequest{})
	if strings.Contains(string(raw), "os_image") {
		t.Fatalf("unchanged: os_image omitted, got %s", raw)
	}
}

func TestCheckDefaultOSApplied(t *testing.T) {
	ubuntu, flatcar := "ubuntu", "flatcar"
	if err := checkDefaultOSApplied(nil, &client.K8sCluster{OsImage: "flatcar"}); err != nil {
		t.Errorf("no change requested: %v", err)
	}
	if err := checkDefaultOSApplied(&ubuntu, &client.K8sCluster{OsImage: "ubuntu"}); err != nil {
		t.Errorf("applied: %v", err)
	}
	// An API older than #2155 ignores os_image in the PATCH.
	err := checkDefaultOSApplied(&ubuntu, &client.K8sCluster{OsImage: flatcar})
	if err == nil || !strings.Contains(err.Error(), "#2155") {
		t.Errorf("ignored change must fail with a clear error, got %v", err)
	}
}
