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

func TestEncryptionMode_ChangeForcesReplacement(t *testing.T) {
	a := encryptionAttr(t)
	req := planmodifier.StringRequest{
		Path:        path.Root("encryption_mode"),
		PlanValue:   types.StringValue("sse_c"),
		StateValue:  types.StringValue("none"),
		ConfigValue: types.StringValue("sse_c"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	// RequiresReplace ignores a create (null state raw): give it a non-null state.
	req.State.Raw = nonNullRaw(t)
	req.Plan.Raw = nonNullRaw(t)
	for _, m := range a.PlanModifiers {
		m.PlanModifyString(context.Background(), req, resp)
	}
	if !resp.RequiresReplace {
		t.Fatal("none -> sse_c must plan a replacement")
	}
}

func TestEncryptionMode_SameValueNoReplacement(t *testing.T) {
	a := encryptionAttr(t)
	req := planmodifier.StringRequest{
		Path:        path.Root("encryption_mode"),
		PlanValue:   types.StringValue("none"),
		StateValue:  types.StringValue("none"),
		ConfigValue: types.StringNull(),
	}
	req.State.Raw = nonNullRaw(t)
	req.Plan.Raw = nonNullRaw(t)
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	for _, m := range a.PlanModifiers {
		m.PlanModifyString(context.Background(), req, resp)
	}
	if resp.RequiresReplace {
		t.Fatal("an unchanged mode must not replace the bucket")
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
