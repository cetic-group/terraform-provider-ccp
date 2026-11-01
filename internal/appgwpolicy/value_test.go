package appgwpolicy

import (
	"encoding/json"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func policyValue(t *testing.T, document string) types.Object {
	t.Helper()
	var input map[string]any
	if err := json.Unmarshal([]byte(document), &input); err != nil {
		t.Fatal(err)
	}
	return attribute(L7Type(), input).(types.Object)
}

const partnerHCL = `{"enabled":true,"policies":{"partner":{"scope":{"path":{"op":"segment_prefix","value":"/adm%69n"}},"allow_rules":{"office":{"match":{"source_cidrs":["203.0.113.7/24","2001:db8::1"],"methods":["GET","GET"]}}},"required_headers":{"partner-id":{"name":"X-Partner-ID","op":"in","values":["a","a","b"]}}}}}`

func TestTypedPolicyWireAndCanonicalRefresh(t *testing.T) {
	value := policyValue(t, partnerHCL)
	p, err := L7(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Policies) != 1 || p.Policies[0].Key != "partner" || p.Policies[0].RequiredHeaders[0].Key != "partner-id" {
		t.Fatalf("stable map keys were not preserved: %#v", p)
	}
	data := normalize(p)
	var response client.AppGWL7Policy
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if !FromL7(value, &response).Equal(value) {
		t.Fatal("canonical response must preserve configured spelling, order and optional omissions")
	}
	imported := FromL7(types.ObjectNull(L7Type().AttrTypes), &response)
	if imported.IsNull() {
		t.Fatal("import dropped protection")
	}
	again, err := L7(imported)
	if err != nil {
		t.Fatal(err)
	}
	if string(normalize(again)) != string(normalize(&response)) {
		t.Fatal("import policy differs from API")
	}
}
func TestOmissionResetUnknownAndMissingResponseAreDistinct(t *testing.T) {
	null := types.ObjectNull(L7Type().AttrTypes)
	empty := policyValue(t, `{}`)
	p, err := L7(null)
	if err != nil || p != nil {
		t.Fatal("omitted policy must be nil")
	}
	p, err = L7(empty)
	if err != nil || p == nil {
		t.Fatal("reset must have a non-nil pointer")
	}
	data, _ := json.Marshal(p)
	if string(data) != "{}" {
		t.Fatalf("reset must serialize as {}, got %s", data)
	}
	if Changed(null, empty, policyValue(t, partnerHCL)) {
		t.Fatal("removing configuration must not clear remote protection")
	}
	if !Changed(empty, empty, policyValue(t, partnerHCL)) {
		t.Fatal("explicit reset must be sent")
	}
	if _, err = L7(types.ObjectUnknown(L7Type().AttrTypes)); err == nil {
		t.Fatal("unknown root must not reset")
	}
	attrs := empty.Attributes()
	attrs["enabled"] = types.BoolUnknown()
	if _, err = L7(types.ObjectValueMust(L7Type().AttrTypes, attrs)); err == nil {
		t.Fatal("unknown nested fields must not reset")
	}
	var d diag.Diagnostics
	if Confirm(&client.AppGWL7Policy{}, (*client.AppGWL7Policy)(nil), &d) || !d.HasError() {
		t.Fatal("missing response must not confirm a reset")
	}
}
func TestEmptyCriteriaAndOverlappingKeysRejected(t *testing.T) {
	for _, input := range []string{
		`{"policies":{"p":{"allow_rules":{"a":{"match":{"source_cidrs":[]}}}}}}`,
		`{"policies":{"p":{"allow_rules":{"a":{"match":{"methods":["GET"],"source_cidrs":[]}}}}}}`,
		`{"policies":{"p":{"allow_rules":{"a":{"match":{"match_all":false}}}}}}`,
		`{"policies":{"p":{"allow_rules":{"a":{"match":{"match_all":true}}},"required_headers":{"a":{"name":"X-Partner","op":"present"}}}}}`,
		`{"policies":{"p":{"required_headers":{"h":{"name":"X-Partner","op":"eq","values":["a,b"]}}}}}`,
	} {
		if _, err := L7(policyValue(t, input)); err == nil {
			t.Fatalf("invalid policy accepted: %s", input)
		}
	}
}
func TestCanonicalPathContract(t *testing.T) {
	for input, want := range map[string]string{"/adm%69n": "/admin", "/hello%20world": "/hello%20world", "/café": "/caf%C3%A9", "/a%2eb": "/a.b"} {
		got, err := CanonicalPath(input)
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", input, got, err)
		}
	}
	for _, input := range []string{"/a%2fb", "/a%5Cb", "/a%25b", "/a//b", "/a/../b", "/a/%2e/b", "/a;b", "/a?b", "/%ff", "/%0a", "/%1", "/%gg"} {
		if _, err := CanonicalPath(input); err == nil {
			t.Fatalf("ambiguous path accepted: %q", input)
		}
	}
}
func TestCanonicalEmptyObjectsAndCIDRsPreserveIntent(t *testing.T) {
	l4 := attribute(L4Type(), map[string]any{"enabled": false, "tcp_443": map[string]any{"allow_cidrs": []any{"203.0.113.7/24"}}}).(types.Object)
	response := &client.AppGWL4Policy{TCP80: &client.AppGWTCPPolicy{}, TCP443: &client.AppGWTCPPolicy{AllowCIDRs: []string{"203.0.113.0/24"}}}
	if !FromL4(l4, response).Equal(l4) {
		t.Fatal("L4 normalization produced a false diff")
	}
	ip := attribute(ClientIPType(), map[string]any{}).(types.Object)
	if !FromClientIP(ip, &client.AppGWClientIPPolicy{Mode: "peer", TrustedProxyCIDRs: []string{}}).Equal(ip) {
		t.Fatal("peer reset produced a false diff")
	}
}
func TestUnknownNestedMapNeverDropsProtections(t *testing.T) {
	fields := map[string]attr.Value{"enabled": types.BoolValue(true), "policies": types.MapUnknown(L7Type().AttrTypes["policies"].(types.MapType).ElemType)}
	if _, err := L7(types.ObjectValueMust(L7Type().AttrTypes, fields)); err == nil {
		t.Fatal("unknown policy map must fail before writing")
	}
}
