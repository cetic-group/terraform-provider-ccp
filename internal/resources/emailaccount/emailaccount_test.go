// Tests for ccp_email_account.
//
// The defect this file exists to prevent is a SILENT one: the platform's update
// route reads an omitted quota as "leave it alone", so a `quota_gb` removed from
// the configuration used to record `null` over a mailbox still reserved — and
// still billed — at its previous size. Nothing failed, and the next plan
// converged on that null, so the gap never surfaced again.
//
// The tests therefore run Create and Update for real, against a mocked API, and
// read both the request BODY and the resulting state.
package emailaccount

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func accountSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&emailAccountResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// object builds a full resource value from the compiled schema: every attribute
// is null except the ones named in `set`.
func object(t *testing.T, s schema.Schema, set map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	objType := s.Type().TerraformType(context.Background()).(tftypes.Object)
	attrs := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, at := range objType.AttributeTypes {
		if v, ok := set[name]; ok {
			attrs[name] = v
		} else {
			attrs[name] = tftypes.NewValue(at, nil)
		}
	}
	for name := range set {
		if _, ok := objType.AttributeTypes[name]; !ok {
			t.Fatalf("no attribute %q in the compiled schema", name)
		}
	}
	return tftypes.NewValue(objType, attrs)
}

func str(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
func num(v int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, v) }
func boolv(v bool) tftypes.Value { return tftypes.NewValue(tftypes.Bool, v) }

func accountBody(quotaBytes int64, displayed string) map[string]any {
	return map[string]any{
		"id": "a-1", "address": "contact@example.com", "quota_bytes": quotaBytes,
		"enabled": true, "enable_imap": true, "enable_pop": false,
		"forward_enabled": false, "forward_destination": []string{}, "forward_keep": true,
		"displayed_name": displayed, "created_at": "2026-09-01T10:00:00Z",
	}
}

// ─── The fix, exercised end to end ───────────────────────────────────────────

// Removing `quota_gb` from the configuration must NOT record null over a
// mailbox that is still reserved — and billed — at its previous size.
//
// With `quota_gb` Optional AND Computed, `UseStateForUnknown` pins the prior
// value at plan time, so this is what the framework hands Update: a plan that
// still carries 20. The provider must then leave the quota alone and keep
// telling the truth in state.
func TestUpdate_RemovingTheQuotaKeepsWhatIsReserved(t *testing.T) {
	const twentyGiB = int64(20) * gibibyte
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/email/accounts/a-1", Status: http.StatusOK,
			Body: accountBody(twentyGiB, "Support")},
		{Method: "GET", Path: "/v1/email/accounts/a-1", Status: http.StatusOK,
			Body: accountBody(twentyGiB, "Support")},
	})
	defer srv.Close()

	s := accountSchema(t)
	prior := object(t, s, map[string]tftypes.Value{
		"id": str("a-1"), "address": str("contact@example.com"),
		"password": str("correct horse battery"),
		"quota_gb": num(20), "quota_bytes": num(twentyGiB),
		"enabled": boolv(true), "enable_imap": boolv(true), "enable_pop": boolv(false),
		"forward_enabled": boolv(false), "forward_keep": boolv(true),
		"displayed_name": str("Sales"),
	})
	// The operator dropped `quota_gb` and renamed the sender. UseStateForUnknown
	// has already resolved the quota back to its prior value.
	planned := object(t, s, map[string]tftypes.Value{
		"id": str("a-1"), "address": str("contact@example.com"),
		"password": str("correct horse battery"),
		"quota_gb": num(20), "quota_bytes": num(twentyGiB),
		"enabled": boolv(true), "enable_imap": boolv(true), "enable_pop": boolv(false),
		"forward_enabled": boolv(false), "forward_keep": boolv(true),
		"displayed_name": str("Support"),
	})

	got := runUpdate(t, srv.URL, s, planned, prior)

	// 1. The quota must not be resent: the plan asks for exactly what is
	//    reserved, and a needless write is a needless risk on a shared server.
	var sent map[string]any
	if err := json.Unmarshal(srv.Calls()[0].Body, &sent); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if _, present := sent["quota_gb"]; present {
		t.Errorf("quota_gb resent although unchanged: %s", srv.Calls()[0].Body)
	}
	if sent["displayed_name"] != "Support" {
		t.Errorf("the actual change was not sent: %s", srv.Calls()[0].Body)
	}

	// 2. State must say what is really reserved — never null.
	if got.QuotaGB.IsNull() {
		t.Fatal("quota_gb recorded as null while 20 GiB stay reserved and billed")
	}
	if got.QuotaGB.ValueInt64() != 20 {
		t.Errorf("quota_gb = %d, want 20", got.QuotaGB.ValueInt64())
	}
	if got.QuotaBytes.ValueInt64() != twentyGiB {
		t.Errorf("quota_bytes = %d, want %d", got.QuotaBytes.ValueInt64(), twentyGiB)
	}
}

// Belt and braces: even if the plan reaches Update with the quota unresolved —
// which is what a plain `Optional` attribute would produce — the state must
// still record what is reserved, never null.
func TestUpdate_AnUnresolvedQuotaNeverRecordsNull(t *testing.T) {
	const twentyGiB = int64(20) * gibibyte
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: accountBody(twentyGiB, "Support")},
		{Method: "GET", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: accountBody(twentyGiB, "Support")},
	})
	defer srv.Close()

	s := accountSchema(t)
	base := map[string]tftypes.Value{
		"id": str("a-1"), "address": str("contact@example.com"),
		"password": str("correct horse battery"), "quota_bytes": num(twentyGiB),
		"enabled": boolv(true), "enable_imap": boolv(true), "enable_pop": boolv(false),
		"forward_enabled": boolv(false), "forward_keep": boolv(true),
	}
	prior := map[string]tftypes.Value{"quota_gb": num(20), "displayed_name": str("Sales")}
	planned := map[string]tftypes.Value{"displayed_name": str("Support")} // quota_gb left null
	for k, v := range base {
		prior[k] = v
		planned[k] = v
	}

	got := runUpdate(t, srv.URL, s, object(t, s, planned), object(t, s, prior))

	var sent map[string]any
	_ = json.Unmarshal(srv.Calls()[0].Body, &sent)
	if _, present := sent["quota_gb"]; present {
		t.Errorf("an unresolved quota must not be sent: %s", srv.Calls()[0].Body)
	}
	if got.QuotaGB.IsNull() {
		t.Fatal("quota_gb recorded as null while 20 GiB stay reserved and billed")
	}
	if got.QuotaGB.ValueInt64() != 20 {
		t.Errorf("quota_gb = %d, want the 20 GiB actually reserved", got.QuotaGB.ValueInt64())
	}
}

// A quota the operator really changed must reach the API.
func TestUpdate_AChangedQuotaIsSent(t *testing.T) {
	const twentyGiB = int64(20) * gibibyte
	const fiftyGiB = int64(50) * gibibyte
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: accountBody(fiftyGiB, "Sales")},
		{Method: "GET", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: accountBody(fiftyGiB, "Sales")},
	})
	defer srv.Close()

	s := accountSchema(t)
	base := map[string]tftypes.Value{
		"id": str("a-1"), "address": str("contact@example.com"),
		"password": str("correct horse battery"), "quota_bytes": num(twentyGiB),
		"enabled": boolv(true), "enable_imap": boolv(true), "enable_pop": boolv(false),
		"forward_enabled": boolv(false), "forward_keep": boolv(true),
		"displayed_name": str("Sales"),
	}
	prior := map[string]tftypes.Value{}
	for k, v := range base {
		prior[k] = v
	}
	prior["quota_gb"] = num(20)
	planned := map[string]tftypes.Value{}
	for k, v := range base {
		planned[k] = v
	}
	planned["quota_gb"] = num(50)

	got := runUpdate(t, srv.URL, s, object(t, s, planned), object(t, s, prior))

	var sent map[string]any
	_ = json.Unmarshal(srv.Calls()[0].Body, &sent)
	if sent["quota_gb"] != float64(50) {
		t.Errorf("a real quota change must be sent: %s", srv.Calls()[0].Body)
	}
	if got.QuotaGB.ValueInt64() != 50 || got.QuotaBytes.ValueInt64() != fiftyGiB {
		t.Errorf("state not aligned on the new quota: gb=%v bytes=%v", got.QuotaGB, got.QuotaBytes)
	}
}

// Changing the password goes through its own route: the platform does not hold
// it, so there is nothing to update — only to reset.
func TestUpdate_PasswordChangeGoesThroughItsOwnRoute(t *testing.T) {
	const oneGiB = int64(1) * gibibyte
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/email/accounts/a-1/password", Status: http.StatusNoContent},
		{Method: "PATCH", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: accountBody(oneGiB, "Sales")},
		{Method: "GET", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: accountBody(oneGiB, "Sales")},
	})
	defer srv.Close()

	s := accountSchema(t)
	base := map[string]tftypes.Value{
		"id": str("a-1"), "address": str("contact@example.com"),
		"quota_gb": num(1), "quota_bytes": num(oneGiB),
		"enabled": boolv(true), "enable_imap": boolv(true), "enable_pop": boolv(false),
		"forward_enabled": boolv(false), "forward_keep": boolv(true),
		"displayed_name": str("Sales"),
	}
	prior := map[string]tftypes.Value{}
	for k, v := range base {
		prior[k] = v
	}
	prior["password"] = str("the old one")
	planned := map[string]tftypes.Value{}
	for k, v := range base {
		planned[k] = v
	}
	planned["password"] = str("the new one")

	got := runUpdate(t, srv.URL, s, object(t, s, planned), object(t, s, prior))

	calls := srv.Calls()
	if calls[0].Path != "/v1/email/accounts/a-1/password" {
		t.Fatalf("the reset must come first, got %s", calls[0].Path)
	}
	var sent map[string]any
	_ = json.Unmarshal(calls[0].Body, &sent)
	if sent["password"] != "the new one" {
		t.Errorf("password not forwarded: %s", calls[0].Body)
	}
	// The general update must never carry a password.
	var patched map[string]any
	_ = json.Unmarshal(calls[1].Body, &patched)
	if _, present := patched["password"]; present {
		t.Errorf("password leaked into the general update: %s", calls[1].Body)
	}
	// The configured value is the only copy Terraform has: it must survive the
	// round trip, since no response carries one.
	if got.Password.ValueString() != "the new one" {
		t.Errorf("password lost from state: %v", got.Password)
	}
}

// `enabled` and the forwarding settings are not accepted by the creation route.
// Left out of the follow-up update, a configuration asking for `enabled = false`
// would come back enabled and Terraform would reject the apply.
func TestCreate_SettingsTheCreationRouteRefusesAreAppliedAfterwards(t *testing.T) {
	const oneGiB = int64(1) * gibibyte
	disabled := accountBody(oneGiB, "")
	disabled["enabled"] = false
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/email/accounts", Status: http.StatusCreated, Body: accountBody(oneGiB, "")},
		{Method: "PATCH", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: disabled},
		{Method: "GET", Path: "/v1/email/accounts/a-1", Status: http.StatusOK, Body: disabled},
	})
	defer srv.Close()

	s := accountSchema(t)
	planned := object(t, s, map[string]tftypes.Value{
		"address": str("contact@example.com"), "password": str("correct horse battery"),
		"enabled": boolv(false), "enable_imap": boolv(true), "enable_pop": boolv(false),
	})

	r := &emailAccountResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	resp := &resource.CreateResponse{State: tfsdk.State{Raw: planned, Schema: s}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Raw: planned, Schema: s}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}

	calls := srv.Calls()
	if len(calls) != 3 {
		t.Fatalf("expected create + update + read back, got %d calls", len(calls))
	}
	var created map[string]any
	_ = json.Unmarshal(calls[0].Body, &created)
	if _, present := created["enabled"]; present {
		t.Errorf("the creation route does not accept `enabled`: %s", calls[0].Body)
	}
	if _, present := created["quota_gb"]; present {
		t.Errorf("an unset quota must not be sent: %s", calls[0].Body)
	}
	var patched map[string]any
	_ = json.Unmarshal(calls[1].Body, &patched)
	if patched["enabled"] != false {
		t.Errorf("`enabled = false` never reached the platform: %s", calls[1].Body)
	}

	var got emailAccountModel
	resp.State.Get(context.Background(), &got)
	if got.Enabled.ValueBool() {
		t.Error("state says enabled while the configuration asked for false")
	}
}

func runUpdate(t *testing.T, url string, s schema.Schema, planned, prior tftypes.Value) emailAccountModel {
	t.Helper()
	ctx := context.Background()
	r := &emailAccountResource{client: client.New(url, "ccp_test_unit", "0.0.0-test")}
	resp := &resource.UpdateResponse{State: tfsdk.State{Raw: prior, Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: planned, Schema: s},
		State: tfsdk.State{Raw: prior, Schema: s},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	var got emailAccountModel
	resp.State.Get(ctx, &got)
	return got
}

// ─── The conversion rules, in isolation ──────────────────────────────────────

func TestQuotaGBFrom(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bytes      int64
		configured types.Int64
		want       types.Int64
	}{
		{"configured value matches to the byte", 20 * gibibyte, types.Int64Value(20), types.Int64Value(20)},
		{"nothing configured — report what is reserved", 20 * gibibyte, types.Int64Null(), types.Int64Value(20)},
		{"plan not resolved yet — report what is reserved", 5 * gibibyte, types.Int64Unknown(), types.Int64Value(5)},
		// A platform default that is not a whole number of gigabytes: rounding
		// DOWN would understate a quota that is billed in full, and invite the
		// operator to write back a smaller number the platform would apply.
		{"not a whole number of gigabytes — round up", 3 * gibibyte / 2, types.Int64Null(), types.Int64Value(2)},
		// A configured value the platform did not honour must not be echoed
		// back: the reserved space is the truth.
		{"configured value not honoured", 20 * gibibyte, types.Int64Value(50), types.Int64Value(20)},
		{"nothing reserved", 0, types.Int64Null(), types.Int64Null()},
	} {
		got := quotaGBFrom(tc.bytes, tc.configured)
		if !got.Equal(tc.want) {
			t.Errorf("%s: quotaGBFrom(%d, %v) = %v, want %v", tc.name, tc.bytes, tc.configured, got, tc.want)
		}
	}
}

func TestQuotaChange(t *testing.T) {
	reserved := types.Int64Value(20 * gibibyte)
	if got := quotaChange(types.Int64Value(20), reserved); got != nil {
		t.Errorf("asking for what is already reserved must send nothing, got %d", *got)
	}
	got := quotaChange(types.Int64Value(50), reserved)
	if got == nil || *got != 50 {
		t.Errorf("a real change must be sent, got %v", got)
	}
	if got := quotaChange(types.Int64Unknown(), reserved); got != nil {
		t.Errorf("an unresolved quota must send nothing, got %d", *got)
	}
	if got := quotaChange(types.Int64Null(), reserved); got != nil {
		t.Errorf("a null quota must send nothing, got %d", *got)
	}
}

// ─── Schema guarantees ───────────────────────────────────────────────────────

// `quota_gb` must stay Optional AND Computed. Plain Optional is what recorded
// null over a mailbox that stayed reserved and billed.
func TestQuotaIsOptionalAndComputed(t *testing.T) {
	attr, ok := accountSchema(t).Attributes["quota_gb"]
	if !ok {
		t.Fatal("quota_gb missing")
	}
	if !attr.IsOptional() {
		t.Error("quota_gb must stay Optional")
	}
	if !attr.IsComputed() {
		t.Error("quota_gb must be Computed: the update route cannot un-set a quota, " +
			"so removing the attribute has to mean `keep what is reserved`")
	}
}

func TestPasswordIsSensitiveAndAddressForcesReplacement(t *testing.T) {
	s := accountSchema(t)
	pw, ok := s.Attributes["password"].(schema.StringAttribute)
	if !ok {
		t.Fatal("password missing")
	}
	if !pw.IsSensitive() {
		t.Error("password must be Sensitive: it is written to state and shown in plans otherwise")
	}
	if !pw.IsRequired() {
		t.Error("password must be Required: the platform has no default and never returns one")
	}
	addr, ok := s.Attributes["address"].(schema.StringAttribute)
	if !ok {
		t.Fatal("address missing")
	}
	if len(addr.PlanModifiers) == 0 {
		t.Error("address must force replacement: renaming a mailbox would move mail already " +
			"delivered and break every forwarding rule pointing at it")
	}
}

// ─── Mapping ─────────────────────────────────────────────────────────────────

func TestStateFrom_NullableFieldsStayNull(t *testing.T) {
	a := &client.EmailAccount{
		ID: "a-1", Address: "contact@example.com", QuotaBytes: gibibyte,
		Enabled: true, EnableIMAP: true, CreatedAt: "2026-09-01T10:00:00Z",
	}
	m, diags := stateFrom(context.Background(), a, types.StringValue("secret"), types.Int64Value(1))
	if diags.HasError() {
		t.Fatalf("stateFrom: %v", diags)
	}
	// A reading that never succeeded is NOT an empty mailbox: it must stay null.
	if !m.UsageBytes.IsNull() || !m.UsageUpdatedAt.IsNull() {
		t.Error("an absent usage reading must stay null, not become zero")
	}
	if !m.Comment.IsNull() || !m.DisplayedName.IsNull() {
		t.Error("absent text fields must stay null, not become empty strings")
	}
	if !m.ClientConfig.IsNull() {
		t.Error("the list shape carries no client config: it must stay null, not become zeroes")
	}
	if m.Password.ValueString() != "secret" {
		t.Error("the password is the only copy Terraform has — it must survive the mapping")
	}
}
