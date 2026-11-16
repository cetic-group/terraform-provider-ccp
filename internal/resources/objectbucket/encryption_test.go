package objectbucket

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// Chiffrement à clé fournie par le client (cetic-cloud-platform#1995) : mode
// choisi à la création, non modifiable.

func encryptionAttr(t *testing.T) rschema.StringAttribute {
	t.Helper()
	var sc resource.SchemaResponse
	(&objectBucketResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sc)
	a, ok := sc.Schema.Attributes["encryption_mode"].(rschema.StringAttribute)
	if !ok {
		t.Fatal("encryption_mode must be a string attribute")
	}
	return a
}

func TestSchema_EncryptionMode(t *testing.T) {
	a := encryptionAttr(t)
	if !a.Optional || !a.Computed || a.Default == nil {
		t.Fatal("encryption_mode must be optional, computed, with a default")
	}
	if len(a.Validators) == 0 || len(a.PlanModifiers) == 0 {
		t.Fatal("encryption_mode needs a format validator and RequiresReplace")
	}
}

func TestEncryptionMode_FormatValidation(t *testing.T) {
	a := encryptionAttr(t)
	check := func(v string) bool {
		req := validator.StringRequest{Path: path.Root("encryption_mode"), ConfigValue: types.StringValue(v)}
		resp := &validator.StringResponse{}
		for _, val := range a.Validators {
			val.ValidateString(context.Background(), req, resp)
		}
		return !resp.Diagnostics.HasError()
	}
	for _, ok := range []string{"none", "sse_c", "future_mode2"} {
		if !check(ok) {
			t.Errorf("%q must be accepted", ok)
		}
	}
	for _, bad := range []string{"SSE-C", "", "1x", "sse c", "Sse_c"} {
		if check(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func replaceVerdict(t *testing.T, state, plan types.String) bool {
	t.Helper()
	a := encryptionAttr(t)
	req := planmodifier.StringRequest{
		Path: path.Root("encryption_mode"), PlanValue: plan, StateValue: state, ConfigValue: plan,
	}
	req.State.Raw = nonNullRaw(t)
	req.Plan.Raw = nonNullRaw(t)
	resp := &planmodifier.StringResponse{PlanValue: plan}
	for _, m := range a.PlanModifiers {
		m.PlanModifyString(context.Background(), req, resp)
	}
	return resp.RequiresReplace
}

func TestEncryptionMode_ReplacementMatrix(t *testing.T) {
	s := types.StringValue
	for _, tc := range []struct {
		name        string
		state, plan types.String
		replace     bool
	}{
		{"none to sse_c", s("none"), s("sse_c"), true},
		{"sse_c to none", s("sse_c"), s("none"), true},
		{"same value", s("none"), s("none"), false},
		// ⚠️ State written before the attribute existed, no refresh: never replace.
		{"null prior, plan none", types.StringNull(), s("none"), false},
		{"null prior, plan sse_c", types.StringNull(), s("sse_c"), false},
		{"unknown plan", s("none"), types.StringUnknown(), false},
		{"unknown prior", types.StringUnknown(), s("none"), false},
	} {
		if got := replaceVerdict(t, tc.state, tc.plan); got != tc.replace {
			t.Errorf("%s: RequiresReplace = %v, want %v", tc.name, got, tc.replace)
		}
	}
}

// Realistic prior state: every other attribute set, encryption_mode null.
func TestEncryptionMode_ExistingStateWithoutAttributePlansNoReplace(t *testing.T) {
	ctx := context.Background()
	var sc resource.SchemaResponse
	(&objectBucketResource{}).Schema(ctx, resource.SchemaRequest{}, &sc)
	st := tfsdk.State{Schema: sc.Schema}
	prior := model(false, nullRules())
	prior.EncryptionMode = types.StringNull()
	if d := st.Set(ctx, &prior); d.HasError() {
		t.Fatal(d)
	}
	var stateVal types.String
	st.GetAttribute(ctx, path.Root("encryption_mode"), &stateVal)
	if !stateVal.IsNull() {
		t.Fatalf("setup: prior state must carry a null mode, got %v", stateVal)
	}
	a := encryptionAttr(t)
	req := planmodifier.StringRequest{
		Path: path.Root("encryption_mode"), PlanValue: types.StringValue("none"), StateValue: stateVal,
		ConfigValue: types.StringNull(),
	}
	req.State.Raw = st.Raw
	req.Plan.Raw = st.Raw
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	for _, m := range a.PlanModifiers {
		m.PlanModifyString(ctx, req, resp)
	}
	if resp.RequiresReplace {
		t.Fatal("an existing bucket must not be replaced when the attribute first appears")
	}
}

func TestCreate_EncryptionModeSentOnlyWhenNotNone(t *testing.T) {
	for _, tc := range []struct {
		mode string
		sent bool
	}{{"none", false}, {"sse_c", true}} {
		var posted map[string]any
		routes := createRoutes()
		routes[0] = testutil.Route{Method: "POST", Path: "/v1/buckets", Status: 201,
			BodyFn: func(t *testing.T, body []byte) (int, any) {
				if err := json.Unmarshal(body, &posted); err != nil {
					t.Fatal(err)
				}
				b := bucketBody(false)
				b["encryption_mode"] = tc.mode
				return 201, b
			}}
		fresh := bucketBody(false)
		fresh["encryption_mode"] = tc.mode
		routes[1].Body, routes[2].Body = fresh, fresh
		srv := testutil.NewTestServer(t, routes)
		plan := model(false, nullRules())
		plan.ID, plan.CorsRevision = types.StringUnknown(), types.StringUnknown()
		plan.EncryptionMode = types.StringValue(tc.mode)
		resp := runCreate(t, srv, plan)
		srv.Close()
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		_, has := posted["encryption_mode"]
		if has != tc.sent {
			t.Fatalf("mode %q: encryption_mode in payload = %v, want %v (%v)", tc.mode, has, tc.sent, posted)
		}
		var got types.String
		resp.State.GetAttribute(context.Background(), path.Root("encryption_mode"), &got)
		if got.ValueString() != tc.mode {
			t.Fatalf("state = %q, want %q", got.ValueString(), tc.mode)
		}
	}
}

// Un serveur plus ancien ne renvoie pas le champ, et un état antérieur au
// champ le porte nul : la lecture doit donner « none », sans dérive.
func TestRead_AbsentEncryptionModeReadsAsNone(t *testing.T) {
	srv := testutil.NewTestServer(t, bucketRoutes())
	defer srv.Close()
	state := model(false, nullRules()) // EncryptionMode null, as in a pre-feature state
	resp := runRead(t, srv, state)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got types.String
	resp.State.GetAttribute(context.Background(), path.Root("encryption_mode"), &got)
	if got.ValueString() != "none" {
		t.Fatalf("absent field must read as none, got %v", got)
	}
}

func TestRead_EncryptionModeFollowsThePlatform(t *testing.T) {
	b := bucketBody(false)
	b["encryption_mode"] = "sse_c"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: b},
		credsRoute(),
	})
	defer srv.Close()
	resp := runRead(t, srv, model(false, nullRules()))
	var got types.String
	resp.State.GetAttribute(context.Background(), path.Root("encryption_mode"), &got)
	if got.ValueString() != "sse_c" {
		t.Fatalf("got %v", got)
	}
}

func nonNullRaw(t *testing.T) tftypes.Value {
	t.Helper()
	return tftypes.NewValue(tftypes.String, "x")
}
