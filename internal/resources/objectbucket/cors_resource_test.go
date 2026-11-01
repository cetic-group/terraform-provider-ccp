package objectbucket

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

const bucketID = "b-1"

func bucketBody(public bool) map[string]any {
	return map[string]any{
		"id": bucketID, "name": "acme", "region": "RNN", "status": "active",
		"is_public": public, "size_bytes": 0, "tags": []string{},
		"created_at": "2026-10-31T10:00:00Z", "updated_at": "2026-10-31T10:00:00Z",
	}
}

func model(public bool, rules types.List) objectBucketResourceModel {
	return objectBucketResourceModel{
		ID: types.StringValue(bucketID), Name: types.StringValue("acme"), Region: types.StringValue("RNN"),
		IsPublic: types.BoolValue(public), CorsRule: rules, Tags: types.ListValueMust(types.StringType, []attr.Value{}),
		EndpointURL: types.StringNull(), SizeBytes: types.Int64Value(0), Status: types.StringValue("active"),
		ErrorMessage: types.StringNull(), AccessKey: types.StringNull(), SecretKey: types.StringNull(),
		CreatedAt: types.StringValue("2026-10-31T10:00:00Z"), UpdatedAt: types.StringValue("2026-10-31T10:00:00Z"),
	}
}

func nullRules() types.List {
	return types.ListNull(types.ObjectType{AttrTypes: corsRuleAttrTypes})
}

func oneRule(t *testing.T, origin string) types.List {
	t.Helper()
	l, d := corsRulesToState(context.Background(), []client.BucketCorsRule{
		{AllowedOrigins: []string{origin}, AllowedMethods: []string{"GET"}, MaxAgeSeconds: 3600},
	})
	if d.HasError() {
		t.Fatal(d)
	}
	return l
}

func runUpdate(t *testing.T, srv *testutil.Server, state, plan objectBucketResourceModel) resource.UpdateResponse {
	t.Helper()
	ctx := context.Background()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sc resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sc)
	st := tfsdk.State{Schema: sc.Schema}
	pl := tfsdk.State{Schema: sc.Schema}
	if d := st.Set(ctx, &state); d.HasError() {
		t.Fatal(d)
	}
	if d := pl.Set(ctx, &plan); d.HasError() {
		t.Fatal(d)
	}
	resp := resource.UpdateResponse{State: st}
	r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan{Raw: pl.Raw, Schema: sc.Schema}, State: st}, &resp)
	return resp
}

func paths(srv *testutil.Server) []string {
	var out []string
	for _, c := range srv.Calls() {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

func credsRoute() testutil.Route {
	return testutil.Route{Method: "GET", Path: "/v1/buckets/" + bucketID + "/credentials", Status: 200,
		Body: map[string]any{"bucket_id": bucketID, "access_key": "ak", "secret_key": "sk", "endpoint_url": "https://s3.test"}}
}

// ⚠️ Revue #105 : un bucket dont le CORS a été posé à la main (console, CLI) ou
// importé a `cors_rule` nul dans l'état. Un Update sans bloc ne doit PAS le
// supprimer — c'est l'inverse de « sans bloc = non géré ».
func TestUpdate_WithoutBlock_LeavesUnmanagedCorsAlone(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(true)},
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(true)},
		credsRoute(),
	})
	defer srv.Close()
	resp := runUpdate(t, srv, model(false, nullRules()), model(true, emptyCorsRules()))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, p := range paths(srv) {
		if strings.Contains(p, "/cors") {
			t.Fatalf("a /cors call was made for a bucket without cors_rule: %v", paths(srv))
		}
	}
}

// Rendre un bucket privé en retirant la règle `*` : l'API refuse le PATCH tant que
// la règle existe → le CORS doit être écrit AVANT.
func TestUpdate_PublicToPrivate_WritesCorsBeforeVisibility(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/" + bucketID + "/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "applied", "revision": "rev-3"}},
		{Method: "DELETE", Path: "/v1/buckets/" + bucketID + "/cors", Status: 204},
		{Method: "PATCH", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(false)},
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(false)},
		credsRoute(),
	})
	defer srv.Close()
	resp := runUpdate(t, srv, model(true, oneRule(t, "*")), model(false, emptyCorsRules()))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := paths(srv)
	del, patch := -1, -1
	for i, p := range got {
		if p == "DELETE /v1/buckets/"+bucketID+"/cors" {
			del = i
		}
		if p == "PATCH /v1/buckets/"+bucketID {
			patch = i
		}
	}
	if del < 0 || patch < 0 || del > patch {
		t.Fatalf("the CORS delete must come before the visibility PATCH: %v", got)
	}
	for _, c := range srv.Calls() {
		if c.Method == "DELETE" && c.Query != "expected_revision=rev-3" {
			t.Fatalf("DELETE must carry the revision, got %q", c.Query)
		}
	}
}

// Dans l'autre sens (privé → public) la visibilité passe d'abord : une règle `*`
// exige un bucket déjà public.
func TestUpdate_PrivateToPublic_WritesVisibilityFirst(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(true)},
		{Method: "GET", Path: "/v1/buckets/" + bucketID + "/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "none", "revision": "r0"}},
		{Method: "PUT", Path: "/v1/buckets/" + bucketID + "/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "applied", "revision": "r1"}},
		{Method: "GET", Path: "/v1/buckets/" + bucketID, Status: 200, Body: bucketBody(true)},
		credsRoute(),
	})
	defer srv.Close()
	resp := runUpdate(t, srv, model(false, nullRules()), model(true, oneRule(t, "*")))
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	got := paths(srv)
	if got[0] != "PATCH /v1/buckets/"+bucketID {
		t.Fatalf("visibility must be written first: %v", got)
	}
}

// ⚠️ Revue #105 (bloquant) : un attribut calculé non configuré restait INCONNU dans
// l'état après apply. Les deux ensembles d'en-têtes ont donc une valeur par défaut.
func TestSchema_HeaderSetsDefaultToEmpty(t *testing.T) {
	var sc resource.SchemaResponse
	(&objectBucketResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sc)
	block := sc.Schema.Blocks["cors_rule"]
	if block == nil {
		t.Fatal("cors_rule block missing")
	}
	for _, name := range []string{"allowed_headers", "expose_headers"} {
		a, ok := block.(rschema.ListNestedBlock).NestedObject.Attributes[name].(rschema.SetAttribute)
		if !ok || a.Default == nil {
			t.Fatalf("%s must have a default so it is never unknown after apply", name)
		}
	}
}

// Un refus du CORS à la création ne doit pas rendre la ressource taintée (le
// bucket serait détruit puis recréé au prochain apply) : un avertissement, pas une
// erreur, et l'état dit la vérité (aucune règle appliquée).
func TestApplyCorsOnCreate_RefusalDoesNotTaint(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/" + bucketID + "/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "none", "revision": "r"}},
		{Method: "PUT", Path: "/v1/buckets/" + bucketID + "/cors", Status: 422,
			Body: map[string]any{"detail": "joker non activé"}},
	})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	plan := model(false, oneRule(t, "https://a.example.com"))
	d := r.applyCorsOnCreate(context.Background(), bucketID, &plan)
	if d.HasError() {
		t.Fatalf("must not be an error (it would taint the bucket): %v", d)
	}
	if d.WarningsCount() != 1 {
		t.Fatalf("want one warning, got %v", d)
	}
	if corsRuleCount(plan.CorsRule) != 0 || plan.CorsRule.IsUnknown() {
		t.Fatal("state must record that no rule was applied")
	}
}

func TestCorsStatusWarning(t *testing.T) {
	msg := "stockage indisponible"
	if _, detail, ok := corsStatusWarning("b", &client.BucketCors{Status: "error", ErrorMessage: &msg}); !ok || !strings.Contains(detail, msg) {
		t.Fatalf("error status must warn with the message: %v %q", ok, detail)
	}
	if _, _, ok := corsStatusWarning("b", &client.BucketCors{Status: "pending"}); !ok {
		t.Fatal("pending status must warn")
	}
	for _, s := range []string{"applied", "none", "unmanaged"} {
		if _, _, ok := corsStatusWarning("b", &client.BucketCors{Status: s}); ok {
			t.Fatalf("status %q must not warn", s)
		}
	}
}

// Les validateurs refusent ce que l'API reformulerait (dérive permanente).
func TestCanonicalCorsOrigin(t *testing.T) {
	ok := []string{"https://app.example.com", "https://app.example.com:8443", "*", "http://localhost:3000"}
	for _, o := range ok {
		got, err := canonicalCorsOrigin(o)
		if err != nil || got != o {
			t.Errorf("%q should be canonical: %q %v", o, got, err)
		}
	}
	notCanonical := map[string]string{
		"https://App.Example.com":     "https://app.example.com",
		"https://app.example.com/":    "https://app.example.com",
		"https://app.example.com:443": "https://app.example.com",
		"http://localhost:80":         "http://localhost",
	}
	for in, want := range notCanonical {
		got, err := canonicalCorsOrigin(in)
		if err != nil || got != want {
			t.Errorf("%q → %q (%v), want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "app.example.com", "https://a.example.com/path", "https://a.example.com?x=1", "https://u:p@a.example.com", strings.Repeat("a", 300)} {
		if _, err := canonicalCorsOrigin(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestCorsHeaderPattern(t *testing.T) {
	for _, h := range []string{"Content-Type", "x-amz-meta-*", "*", "ETag"} {
		if !corsHeaderPattern.MatchString(h) {
			t.Errorf("%q should match", h)
		}
	}
	for _, h := range []string{"Content Type", "a:b", "é"} {
		if corsHeaderPattern.MatchString(h) {
			t.Errorf("%q should not match", h)
		}
	}
}
