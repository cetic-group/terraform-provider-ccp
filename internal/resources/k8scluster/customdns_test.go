package k8scluster

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/ccks"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func dnsList(entries ...[]string) types.List {
	elems := []attr.Value{}
	for _, e := range entries {
		servers := []attr.Value{}
		for _, s := range e[1:] {
			servers = append(servers, types.StringValue(s))
		}
		elems = append(elems, types.ObjectValueMust(ccks.CustomDNSEntryAttrTypes, map[string]attr.Value{
			"domain":  types.StringValue(e[0]),
			"servers": types.ListValueMust(types.StringType, servers),
		}))
	}
	return types.ListValueMust(ccks.CustomDNSElemType, elems)
}

var dnsNull = types.ListNull(ccks.CustomDNSElemType)

// The API stores the domain in lower case without its final dot: the state
// keeps the configured spelling when it is the same list (no "inconsistent
// result", no diff); an empty API list is null unless `[]` is configured.
func TestCustomDNSValue_SameListKeepsSpelling(t *testing.T) {
	ctx := context.Background()
	api := []client.K8sCustomDNSEntry{{Domain: "corp.example.com", Servers: []string{"10.0.1.10", "10.0.1.11"}}}
	prior := dnsList([]string{" Corp.Example.COM. ", "10.0.1.10", " 10.0.1.11"})
	if got, d := customDNSValue(ctx, api, prior); d.HasError() || !got.Equal(prior) {
		t.Fatalf("same list must keep the prior value, got %s", got)
	}
	// Servers swapped: the primary changed, it is a different list.
	swapped := dnsList([]string{"corp.example.com", "10.0.1.11", "10.0.1.10"})
	if got, _ := customDNSValue(ctx, api, swapped); !got.Equal(dnsList([]string{"corp.example.com", "10.0.1.10", "10.0.1.11"})) {
		t.Fatalf("server order matters, got %s", got)
	}
	// Set outside Terraform, none configured: read back (the next plan removes it).
	if got, _ := customDNSValue(ctx, api, dnsNull); len(got.Elements()) != 1 {
		t.Fatalf("drift not read back: %s", got)
	}
	// None on the API.
	if got, _ := customDNSValue(ctx, nil, dnsNull); !got.IsNull() {
		t.Fatalf("no custom DNS, nothing configured: null, got %s", got)
	}
	empty := dnsList()
	if got, _ := customDNSValue(ctx, nil, empty); !got.Equal(empty) {
		t.Fatalf("`custom_dns = []` configured: kept, got %s", got)
	}
	if got, _ := customDNSValue(ctx, nil, prior); !got.IsNull() {
		t.Fatalf("removed outside Terraform: null, got %s", got)
	}
}

// Authoritative like `oidc`: the PUT carries the whole list, `[]` when the
// attribute is removed; never sent when the list is the same once normalised;
// the cluster is checked active first.
func TestApplyCustomDNS_SendsOnlyOnChange(t *testing.T) {
	ctx := context.Background()
	current := []client.K8sCustomDNSEntry{{Domain: "corp.example.com", Servers: []string{"10.0.1.10"}}}

	// No route at all: any call fails the test.
	srv := testutil.NewTestServer(t, nil)
	r := &k8sResource{client: client.New(srv.URL, "k", "test")}
	if got, d := r.applyCustomDNS(ctx, "cl-1", dnsList([]string{"CORP.example.com.", "10.0.1.10"}), current, time.Minute); got != nil || d.HasError() {
		t.Fatalf("same list must send nothing (%v, %v)", got, d)
	}
	if got, d := r.applyCustomDNS(ctx, "cl-1", dnsNull, nil, time.Minute); got != nil || d.HasError() {
		t.Fatal("no custom DNS before nor after: nothing sent")
	}
	if got, d := r.applyCustomDNS(ctx, "cl-1", dnsList(), nil, time.Minute); got != nil || d.HasError() {
		t.Fatal("`[]` and none are the same: nothing sent")
	}
	srv.Close()

	srv = testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Status: http.StatusOK, Body: activeV2143("standard")},
		{Method: "PUT", Path: "/v1/k8s/clusters/cl-1/custom-dns", Status: http.StatusOK, Body: activeV2143("standard")},
		{Method: "GET", Path: "/v1/k8s/clusters/cl-1", Status: http.StatusOK, Body: activeV2143("standard")},
		{Method: "PUT", Path: "/v1/k8s/clusters/cl-1/custom-dns", Status: http.StatusOK, Body: activeV2143("standard")},
	})
	defer srv.Close()
	r = &k8sResource{client: client.New(srv.URL, "k", "test")}
	changed := dnsList([]string{"corp.example.com", "10.0.1.10", "10.0.1.11"}, []string{"lab.example", "10.0.2.5"})
	if _, d := r.applyCustomDNS(ctx, "cl-1", changed, current, time.Minute); d.HasError() {
		t.Fatal(d)
	}
	if _, d := r.applyCustomDNS(ctx, "cl-1", dnsNull, current, time.Minute); d.HasError() {
		t.Fatal(d)
	}
	calls := srv.Calls()
	if len(calls) != 4 || calls[0].Method != "GET" || calls[1].Method != "PUT" || calls[3].Method != "PUT" {
		t.Fatalf("calls = %+v", calls)
	}
	want := `{"custom_dns":[{"domain":"corp.example.com","servers":["10.0.1.10","10.0.1.11"]},{"domain":"lab.example","servers":["10.0.2.5"]}]}`
	if string(calls[1].Body) != want {
		t.Errorf("PUT body = %s\nwant %s", calls[1].Body, want)
	}
	if string(calls[3].Body) != `{"custom_dns":[]}` {
		t.Errorf("removal PUT body = %s, want an empty list", calls[3].Body)
	}
}

// `private_dns` is read from the API response, null when the API has none.
func TestStateFromAPI_PrivateDNS(t *testing.T) {
	ctx := context.Background()
	c := &client.K8sCluster{ID: "cl-1", Status: "active", Plan: "starter"}
	m, ds := stateFromAPI(ctx, c, nil)
	if len(ds) > 0 || !m.PrivateDNS.IsNull() || !m.CustomDNS.IsNull() {
		t.Fatalf("no private DNS: null (%v, %s, %s)", ds, m.PrivateDNS, m.CustomDNS)
	}
	ip, reason := "10.0.0.53", "resolver_not_ready"
	c.PrivateDNS = &client.K8sClusterPrivateDNS{
		Status: "pending", ResolverIP: &ip, PendingReason: &reason,
		Zones:         []client.K8sPrivateDNSZone{{Name: "internal.example", Status: "pending"}},
		Excluded:      []client.K8sPrivateDNSExcluded{{Zone: "cluster.local", Reason: "cluster_domain"}},
		CustomDomains: []client.K8sPrivateDNSCustomDomain{{Domain: "corp.example.com", Servers: []string{"10.0.1.10"}, Status: "applied"}},
	}
	m, ds = stateFromAPI(ctx, c, nil)
	if len(ds) > 0 || m.PrivateDNS.IsNull() {
		t.Fatalf("private DNS not read: %v", ds)
	}
	a := m.PrivateDNS.Attributes()
	if a["status"].(types.String).ValueString() != "pending" ||
		a["pending_reason"].(types.String).ValueString() != "resolver_not_ready" ||
		len(a["zones"].(types.List).Elements()) != 1 || len(a["custom_domains"].(types.List).Elements()) != 1 ||
		!a["error"].(types.String).IsNull() {
		t.Fatalf("private_dns = %s", m.PrivateDNS)
	}
}

// The schema refuses what the platform would refuse for sure: more than 5
// domains, 0 or 3 servers, a server that is not an IPv4 address. Computed
// `private_dns` has no UseStateForUnknown (it moves on its own).
func TestSchema_CustomDNS(t *testing.T) {
	ctx := context.Background()
	resp := &resource.SchemaResponse{}
	New().Schema(ctx, resource.SchemaRequest{}, resp)
	cd, ok := resp.Schema.Attributes["custom_dns"].(schema.ListNestedAttribute)
	if !ok || !cd.Optional || cd.Computed {
		t.Fatal("custom_dns must be an Optional (not Computed) list: removing it removes the custom DNS")
	}
	servers := cd.NestedObject.Attributes["servers"].(schema.ListAttribute)
	check := func(vs []validator.List, l types.List) bool {
		for _, v := range vs {
			r := &validator.ListResponse{}
			v.ValidateList(ctx, validator.ListRequest{ConfigValue: l}, r)
			if r.Diagnostics.HasError() {
				return false
			}
		}
		return true
	}
	strs := func(ss ...string) types.List {
		vs := []attr.Value{}
		for _, s := range ss {
			vs = append(vs, types.StringValue(s))
		}
		return types.ListValueMust(types.StringType, vs)
	}
	for _, tc := range []struct {
		l  types.List
		ok bool
	}{
		{strs("10.0.1.10"), true},
		{strs("10.0.1.10", "10.0.1.11"), true},
		{strs(), false},
		{strs("10.0.1.10", "10.0.1.11", "10.0.1.12"), false},
		{strs("10.0.1.10", "10.0.1.10"), false},
		{strs("dns.corp"), false},
		{strs("10.0.01.1"), false},
		{strs("10.0.1.256"), false},
	} {
		if got := check(servers.Validators, tc.l); got != tc.ok {
			t.Errorf("servers %s: valid = %v, want %v", tc.l, got, tc.ok)
		}
	}
	six := [][]string{}
	for i := 0; i < 6; i++ {
		six = append(six, []string{"d" + string(rune('a'+i)) + ".example", "10.0.1.10"})
	}
	if check(cd.Validators, dnsList(six...)) {
		t.Error("6 domains must be refused")
	}
	pd, ok := resp.Schema.Attributes["private_dns"].(schema.SingleNestedAttribute)
	if !ok || !pd.Computed || pd.Optional || len(pd.PlanModifiers) != 0 {
		t.Fatal("private_dns must be Computed only, without plan modifier")
	}
}

// Sent with the creation (validated before any provisioning), omitted
// when not configured (an API older than #2181 sees no new field).
func TestCreateRequest_CustomDNS(t *testing.T) {
	ctx := context.Background()
	entries, d := customDNSFromList(ctx, dnsList([]string{"corp.example.com", "10.0.1.10"}))
	if d.HasError() {
		t.Fatal(d)
	}
	raw, _ := json.Marshal(client.K8sClusterCreateRequest{CustomDNS: entries})
	if !strings.Contains(string(raw), `"custom_dns":[{"domain":"corp.example.com","servers":["10.0.1.10"]}]`) {
		t.Fatalf("create body = %s", raw)
	}
	raw, _ = json.Marshal(client.K8sClusterCreateRequest{})
	if strings.Contains(string(raw), "custom_dns") {
		t.Fatalf("no custom DNS: field omitted, got %s", raw)
	}
}
