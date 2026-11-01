package objectbucket

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func sampleRules() []client.BucketCorsRule {
	desc := "app"
	return []client.BucketCorsRule{
		{AllowedOrigins: []string{"https://b.example.com", "https://a.example.com"},
			AllowedMethods: []string{"PUT", "POST"}, AllowedHeaders: []string{"Content-Type"},
			ExposeHeaders: []string{"ETag"}, MaxAgeSeconds: 600, Description: &desc},
		{AllowedOrigins: []string{"https://c.example.com"}, AllowedMethods: []string{"GET"},
			MaxAgeSeconds: 3600},
	}
}

// State → plan → API must be lossless, and ordering of sets must not matter
// (sets are sorted on the way out so the request is deterministic).
func TestCorsRules_RoundTrip(t *testing.T) {
	ctx := context.Background()
	list, d := corsRulesToState(ctx, sampleRules())
	if d.HasError() {
		t.Fatal(d)
	}
	if corsRuleCount(list) != 2 {
		t.Fatalf("want 2 rules, got %d", corsRuleCount(list))
	}
	back, d := corsRulesFromPlan(ctx, list)
	if d.HasError() {
		t.Fatal(d)
	}
	if back[0].AllowedOrigins[0] != "https://a.example.com" {
		t.Fatalf("origins not sorted deterministically: %v", back[0].AllowedOrigins)
	}
	if back[0].MaxAgeSeconds != 600 || back[0].Description == nil || *back[0].Description != "app" {
		t.Fatalf("scalar fields lost: %+v", back[0])
	}
	if back[1].Description != nil || len(back[1].AllowedHeaders) != 0 {
		t.Fatalf("empty fields must stay empty: %+v", back[1])
	}
}

func TestCorsRules_NullAndEmpty(t *testing.T) {
	if corsRuleCount(types.ListNull(types.ObjectType{AttrTypes: corsRuleAttrTypes})) != 0 {
		t.Fatal("null list must count as zero rules")
	}
	rules, d := corsRulesFromPlan(context.Background(), types.ListNull(types.ObjectType{AttrTypes: corsRuleAttrTypes}))
	if d.HasError() || rules != nil {
		t.Fatalf("null plan → no rules, got %v %v", rules, d)
	}
}

// writeCors reads the revision first and echoes it, so a concurrent change is
// refused (409) rather than overwritten.
func TestWriteCors_SendsRevision(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/b-1/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "none", "revision": "rev-7"}},
		{Method: "PUT", Path: "/v1/buckets/b-1/cors", Status: 200,
			BodyFn: func(t *testing.T, body []byte) (int, any) {
				var got map[string]any
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatal(err)
				}
				if got["expected_revision"] != "rev-7" {
					t.Errorf("expected_revision = %v", got["expected_revision"])
				}
				return 200, map[string]any{"rules": got["rules"], "status": "pending", "revision": "rev-8"}
			}},
	})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	if _, err := r.writeCors(context.Background(), "b-1", sampleRules(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCors_EmptyDeletesWithRevision(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/b-1/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "applied", "revision": "rev-3"}},
		{Method: "DELETE", Path: "/v1/buckets/b-1/cors", Status: 204},
	})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	if _, err := r.writeCors(context.Background(), "b-1", nil, ""); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCors_NothingToDelete(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/b-1/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "none", "revision": "r"}},
	})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	if _, err := r.writeCors(context.Background(), "b-1", nil, ""); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCors_ConflictSurfaces(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/buckets/b-1/cors", Status: 200,
			Body: map[string]any{"rules": []any{}, "status": "none", "revision": "r"}},
		{Method: "PUT", Path: "/v1/buckets/b-1/cors", Status: 409,
			Body: map[string]any{"detail": "Les règles CORS ont changé"}},
	})
	defer srv.Close()
	r := &objectBucketResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	_, err := r.writeCors(context.Background(), "b-1", sampleRules(), "")
	if err == nil || !client.IsConflict(err) {
		t.Fatalf("want a conflict error, got %v", err)
	}
}
