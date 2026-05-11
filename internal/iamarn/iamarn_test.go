// Tests porting `apps/api/tests/test_iam_arn.py` (66 distinct cases + helpers,
// total 75 t.Run invocations) to Go. Behaviour parity with the Python parser
// is the contract — if a test fails here it means the Go port diverges from
// the source-of-truth implementation in `app/services/iam_arn.py`.
package iamarn

import (
	"errors"
	"testing"
)

const (
	t1 = "11111111-2222-3333-4444-555555555555"
	t2 = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// ─── ParseArn — valid ────────────────────────────────────────────────────────

func TestParseArn_Valid(t *testing.T) {
	cases := []struct {
		name     string
		arn      string
		expected ArnParts
	}{
		{
			"registry regional",
			"arn:ccp:registry:rnn:" + t1 + ":registry/abc",
			ArnParts{Service: "registry", Region: "rnn", TenantID: t1, ResourcePath: "registry/abc"},
		},
		{
			"bucket regional",
			"arn:ccp:bucket:par:" + t1 + ":bucket/my-bkt",
			ArnParts{Service: "bucket", Region: "par", TenantID: t1, ResourcePath: "bucket/my-bkt"},
		},
		{
			"k8s cluster + sub-resource",
			"arn:ccp:k8s:abj:" + t1 + ":cluster/uuid/node-pool/np1",
			ArnParts{Service: "k8s", Region: "abj", TenantID: t1, ResourcePath: "cluster/uuid/node-pool/np1"},
		},
		{
			"built-in role (tenant_id and region empty)",
			"arn:ccp:iam:::role/AdminAll",
			ArnParts{Service: "iam", Region: "", TenantID: "", ResourcePath: "role/AdminAll"},
		},
		{
			"custom role tenant-scoped (region empty)",
			"arn:ccp:iam::" + t1 + ":role/MyCustomRole",
			ArnParts{Service: "iam", Region: "", TenantID: t1, ResourcePath: "role/MyCustomRole"},
		},
		{
			"billing global (region empty)",
			"arn:ccp:billing::" + t1 + ":invoice/inv-123",
			ArnParts{Service: "billing", Region: "", TenantID: t1, ResourcePath: "invoice/inv-123"},
		},
		{
			"dbaas engine pg",
			"arn:ccp:dbaas:rnn:" + t1 + ":instance/pg/db1",
			ArnParts{Service: "dbaas", Region: "rnn", TenantID: t1, ResourcePath: "instance/pg/db1"},
		},
		{
			"wildcard tenant_id",
			"arn:ccp:registry:rnn:*:registry/foo",
			ArnParts{Service: "registry", Region: "rnn", TenantID: "*", ResourcePath: "registry/foo"},
		},
		{
			"wildcard total in resource_path",
			"arn:ccp:bucket:rnn:" + t1 + ":*",
			ArnParts{Service: "bucket", Region: "rnn", TenantID: t1, ResourcePath: "*"},
		},
		{
			"wildcard region",
			"arn:ccp:registry:*:" + t1 + ":registry/foo",
			ArnParts{Service: "registry", Region: "*", TenantID: t1, ResourcePath: "registry/foo"},
		},
		{
			"service wildcard",
			"arn:ccp:*:rnn:" + t1 + ":registry/foo",
			ArnParts{Service: "*", Region: "rnn", TenantID: t1, ResourcePath: "registry/foo"},
		},
		{
			"vpc + sub-vnet",
			"arn:ccp:vpc:par:" + t1 + ":vpc/vp1/vnet/vn1",
			ArnParts{Service: "vpc", Region: "par", TenantID: t1, ResourcePath: "vpc/vp1/vnet/vn1"},
		},
		{
			"object S3 with slashes in key",
			"arn:ccp:bucket:rnn:" + t1 + ":bucket/b1/object/path/to/file.txt",
			ArnParts{Service: "bucket", Region: "rnn", TenantID: t1, ResourcePath: "bucket/b1/object/path/to/file.txt"},
		},
		{
			"question-mark wildcard",
			"arn:ccp:registry:rnn:" + t1 + ":registry/abc?",
			ArnParts{Service: "registry", Region: "rnn", TenantID: t1, ResourcePath: "registry/abc?"},
		},
		{
			"service account",
			"arn:ccp:iam::" + t1 + ":service-account/sa1",
			ArnParts{Service: "iam", Region: "", TenantID: t1, ResourcePath: "service-account/sa1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseArn(tc.arn)
			if err != nil {
				t.Fatalf("ParseArn(%q) unexpected err: %v", tc.arn, err)
			}
			if got != tc.expected {
				t.Errorf("ParseArn(%q) = %+v, want %+v", tc.arn, got, tc.expected)
			}
		})
	}
}

// ─── ParseArn — invalid ─────────────────────────────────────────────────────

func TestParseArn_Invalid(t *testing.T) {
	cases := []struct {
		name string
		arn  string
	}{
		{"empty", ""},
		{"1 segment", "arn"},
		{"2 segments", "arn:ccp"},
		{"3 segments", "arn:ccp:registry"},
		{"4 segments", "arn:ccp:registry:rnn"},
		{"5 segments (not enough)", "arn:ccp:registry:rnn:tid"},
		{"resource_path empty", "arn:ccp:registry:rnn:tid:"},
		{"wrong prefix", "aws:ccp:registry:rnn:" + t1 + ":foo"},
		{"wrong namespace", "arn:aws:registry:rnn:" + t1 + ":foo"},
		{"service empty", "arn:ccp::rnn:" + t1 + ":foo"},
		{"unknown service (no wildcard)", "arn:ccp:unknown:rnn:" + t1 + ":foo"},
		{"unknown region", "arn:ccp:registry:zzz:" + t1 + ":foo"},
		{"tenant_id not uuid", "arn:ccp:registry:rnn:not-a-uuid:foo"},
		{"space in resource_path", "arn:ccp:registry:rnn:" + t1 + ":foo bar"},
		{"space in region", "arn:ccp:registry:r n n:" + t1 + ":foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseArn(tc.arn)
			if err == nil {
				t.Errorf("ParseArn(%q) expected error, got nil", tc.arn)
			}
			if err != nil && !errors.Is(err, ErrInvalidArn) {
				t.Errorf("ParseArn(%q) err not ErrInvalidArn: %v", tc.arn, err)
			}
		})
	}
}

func TestParseArn_UppercaseRegionNormalized(t *testing.T) {
	parts, err := ParseArn("arn:ccp:registry:RNN:" + t1 + ":registry/foo")
	if err != nil {
		t.Fatalf("ParseArn: %v", err)
	}
	if parts.Region != "rnn" {
		t.Errorf("expected region=rnn, got %q", parts.Region)
	}
}

// ─── BuildArn ───────────────────────────────────────────────────────────────

func TestBuildArn_Basic(t *testing.T) {
	out, err := BuildArn("registry", "rnn", t1, "registry/abc")
	if err != nil {
		t.Fatalf("BuildArn: %v", err)
	}
	want := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if out != want {
		t.Errorf("BuildArn = %q, want %q", out, want)
	}
}

func TestBuildArn_NormalizesRegion(t *testing.T) {
	out, _ := BuildArn("registry", "RNN", t1, "registry/abc")
	want := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if out != want {
		t.Errorf("BuildArn = %q, want %q", out, want)
	}
}

func TestBuildArn_EmptyRegion(t *testing.T) {
	out, _ := BuildArn("iam", "", t1, "role/Foo")
	want := "arn:ccp:iam::" + t1 + ":role/Foo"
	if out != want {
		t.Errorf("BuildArn = %q, want %q", out, want)
	}
}

func TestBuildArn_BuiltIn(t *testing.T) {
	out, _ := BuildArn("iam", "", "", "role/AdminAll")
	want := "arn:ccp:iam:::role/AdminAll"
	if out != want {
		t.Errorf("BuildArn = %q, want %q", out, want)
	}
}

func TestBuildArn_RefusesColonInSegment(t *testing.T) {
	_, err := BuildArn("registry:bad", "rnn", t1, "registry/abc")
	if err == nil {
		t.Errorf("expected error on `:` in service segment")
	}
}

func TestBuildArn_RefusesColonInResourcePath(t *testing.T) {
	_, err := BuildArn("registry", "rnn", t1, "registry/foo:bar")
	if err == nil {
		t.Errorf("expected error on `:` in resource_path")
	}
}

// ─── MatchArn ───────────────────────────────────────────────────────────────

func TestMatchArn_Exact(t *testing.T) {
	a := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn(a, a) {
		t.Errorf("exact match failed")
	}
}

func TestMatchArn_WildcardGlobal(t *testing.T) {
	if !MatchArn("*", "arn:ccp:registry:rnn:"+t1+":registry/abc") {
		t.Errorf("`*` should match any arn")
	}
	if !MatchArn("*", "arn:ccp:iam:::role/AdminAll") {
		t.Errorf("`*` should match built-in arn")
	}
}

func TestMatchArn_WildcardResourcePath(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn("arn:ccp:registry:rnn:"+t1+":*", target) {
		t.Errorf("wildcard resource_path should match")
	}
	if !MatchArn("arn:ccp:registry:rnn:"+t1+":registry/*", target) {
		t.Errorf("wildcard sub-resource should match")
	}
}

func TestMatchArn_WildcardTenant(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn("arn:ccp:registry:rnn:*:registry/abc", target) {
		t.Errorf("wildcard tenant should match")
	}
	if !MatchArn("arn:ccp:registry:rnn:*:*", target) {
		t.Errorf("wildcard tenant + resource should match")
	}
}

func TestMatchArn_WildcardRegion(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn("arn:ccp:registry:*:"+t1+":registry/abc", target) {
		t.Errorf("wildcard region should match")
	}
}

func TestMatchArn_WildcardService(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn("arn:ccp:*:rnn:"+t1+":registry/abc", target) {
		t.Errorf("wildcard service should match")
	}
}

func TestMatchArn_DifferentServiceNoMatch(t *testing.T) {
	pat := "arn:ccp:registry:rnn:" + t1 + ":*"
	tgt := "arn:ccp:bucket:rnn:" + t1 + ":bucket/b1"
	if MatchArn(pat, tgt) {
		t.Errorf("services should not cross-match")
	}
}

func TestMatchArn_DifferentRegionNoMatch(t *testing.T) {
	pat := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	tgt := "arn:ccp:registry:par:" + t1 + ":registry/abc"
	if MatchArn(pat, tgt) {
		t.Errorf("regions should not cross-match")
	}
}

func TestMatchArn_DifferentTenantNoMatch(t *testing.T) {
	pat := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	tgt := "arn:ccp:registry:rnn:" + t2 + ":registry/abc"
	if MatchArn(pat, tgt) {
		t.Errorf("tenants should not cross-match")
	}
}

func TestMatchArn_DifferentResourceNoMatch(t *testing.T) {
	pat := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	tgt := "arn:ccp:registry:rnn:" + t1 + ":registry/xyz"
	if MatchArn(pat, tgt) {
		t.Errorf("different resources should not match")
	}
}

func TestMatchArn_GlobResourcePathTraversesSlashes(t *testing.T) {
	target := "arn:ccp:bucket:rnn:" + t1 + ":bucket/b1/object/foo/bar.txt"
	for _, pat := range []string{
		"arn:ccp:bucket:rnn:" + t1 + ":*",
		"arn:ccp:bucket:rnn:" + t1 + ":bucket/*",
		"arn:ccp:bucket:rnn:" + t1 + ":bucket/b1/object/*",
		"arn:ccp:bucket:rnn:" + t1 + ":bucket/b1/*/foo/*.txt",
	} {
		t.Run(pat, func(t *testing.T) {
			if !MatchArn(pat, target) {
				t.Errorf("pattern %q should match target %q", pat, target)
			}
		})
	}
}

func TestMatchArn_QuestionMarkWildcard(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn("arn:ccp:registry:rnn:"+t1+":registry/ab?", target) {
		t.Errorf("`ab?` should match `abc`")
	}
}

func TestMatchArn_QuestionMarkNoMatchMultiChar(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abcd"
	if MatchArn("arn:ccp:registry:rnn:"+t1+":registry/ab?", target) {
		t.Errorf("`ab?` should NOT match multi-char `abcd`")
	}
}

func TestMatchArn_BuiltInGlobalPattern(t *testing.T) {
	target := "arn:ccp:iam:::role/AdminAll"
	if !MatchArn(target, target) {
		t.Errorf("exact match on built-in failed")
	}
	// fnmatch("", "*") returns True (any pattern matches the empty string)
	if !MatchArn("arn:ccp:iam::*:role/AdminAll", target) {
		t.Errorf("`*` tenant should match empty tenant")
	}
	// Strict UUID pattern should NOT match a built-in (empty tenant)
	if MatchArn("arn:ccp:iam::"+t1+":role/AdminAll", target) {
		t.Errorf("specific tenant pattern should not match built-in")
	}
}

func TestMatchArn_RefusesMalformed(t *testing.T) {
	if MatchArn("arn:ccp:registry:rnn:"+t1+":*", "not-an-arn") {
		t.Errorf("malformed target should yield false")
	}
	if MatchArn("arn:ccp:registry:rnn:"+t1+":*", "") {
		t.Errorf("empty target should yield false")
	}
	if MatchArn("malformed", "malformed") {
		t.Errorf("malformed pattern + target should yield false")
	}
	if MatchArn("foo:bar", "arn:ccp:registry:rnn:"+t1+":registry/abc") {
		t.Errorf("malformed pattern should yield false")
	}
}

func TestMatchesAny(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	patterns := []string{
		"arn:ccp:bucket:rnn:" + t1 + ":*",
		"arn:ccp:registry:par:" + t1 + ":*",
		"arn:ccp:registry:rnn:" + t1 + ":registry/*",
	}
	if !MatchesAny(patterns, target) {
		t.Errorf("MatchesAny should have matched at least one")
	}
}

func TestMatchesAny_NoneMatch(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	patterns := []string{
		"arn:ccp:bucket:rnn:" + t1 + ":*",
		"arn:ccp:registry:par:" + t1 + ":*",
	}
	if MatchesAny(patterns, target) {
		t.Errorf("MatchesAny should not have matched")
	}
}

func TestMatchesAny_EmptyList(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if MatchesAny(nil, target) {
		t.Errorf("MatchesAny([]) should return false")
	}
}

// ─── Helpers DRY ────────────────────────────────────────────────────────────

func TestArnForRegistry(t *testing.T) {
	out, _ := ArnForRegistry(t1, "rnn", "reg-uuid")
	want := "arn:ccp:registry:rnn:" + t1 + ":registry/reg-uuid"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
	if !MatchArn("arn:ccp:registry:rnn:"+t1+":*", out) {
		t.Errorf("registry arn does not match wildcard pattern")
	}
}

func TestArnForRepository(t *testing.T) {
	out, _ := ArnForRepository(t1, "rnn", "reg-uuid", "myorg/myimg")
	want := "arn:ccp:registry:rnn:" + t1 + ":registry/reg-uuid/repository/myorg/myimg"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestArnForBucket(t *testing.T) {
	out, _ := ArnForBucket(t1, "par", "bkt")
	want := "arn:ccp:bucket:par:" + t1 + ":bucket/bkt"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestArnForObject(t *testing.T) {
	out, _ := ArnForObject(t1, "par", "bkt", "path/to/file.tar.gz")
	want := "arn:ccp:bucket:par:" + t1 + ":bucket/bkt/object/path/to/file.tar.gz"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestArnForDbaasInstance_Engines(t *testing.T) {
	pg, _ := ArnForDbaasInstance(t1, "rnn", "pg", "db1")
	if pg != "arn:ccp:dbaas:rnn:"+t1+":instance/pg/db1" {
		t.Errorf("pg arn mismatch: %q", pg)
	}
	valkey, _ := ArnForDbaasInstance(t1, "rnn", "valkey", "db2")
	if valkey != "arn:ccp:dbaas:rnn:"+t1+":instance/valkey/db2" {
		t.Errorf("valkey arn mismatch: %q", valkey)
	}
}

func TestArnForRoleTenantScoped(t *testing.T) {
	out, _ := ArnForRole(t1, "MyCustom")
	want := "arn:ccp:iam::" + t1 + ":role/MyCustom"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestArnForBuiltInRoleGlobal(t *testing.T) {
	out, _ := ArnForBuiltInRole("AdminAll")
	want := "arn:ccp:iam:::role/AdminAll"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestArnBuiltInsMatchWildcardPattern(t *testing.T) {
	target, _ := ArnForBuiltInRole("AdminAll")
	if !MatchArn("arn:ccp:iam::*:role/*", target) {
		t.Errorf("wildcard tenant+role should match built-in")
	}
	if !MatchArn("arn:ccp:iam:::role/*", target) {
		t.Errorf("empty tenant + wildcard role should match")
	}
	if !MatchArn("arn:ccp:iam:::role/AdminAll", target) {
		t.Errorf("exact built-in should match")
	}
	if MatchArn("arn:ccp:iam:::role/Member", target) {
		t.Errorf("different built-in should not match")
	}
}

// ─── Edge cases ─────────────────────────────────────────────────────────────

func TestMatchArn_WildcardPatternResourcePathDoesNotMatchShortTarget(t *testing.T) {
	target := "arn:ccp:bucket:rnn:" + t1 + ":bucket/b1"
	if MatchArn("arn:ccp:bucket:rnn:"+t1+":bucket/b1/object/*", target) {
		t.Errorf("longer pattern should not match shorter target")
	}
}

func TestMatchArn_RepositorySpecific(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/r1/repository/team/svc"
	if !MatchArn("arn:ccp:registry:rnn:"+t1+":registry/r1/repository/team/*", target) {
		t.Errorf("expected repo team/* to match")
	}
	if MatchArn("arn:ccp:registry:rnn:"+t1+":registry/r1/repository/other/*", target) {
		t.Errorf("other team should not match")
	}
}

func TestParseThenBuildRoundtrip(t *testing.T) {
	arns := []string{
		"arn:ccp:registry:rnn:" + t1 + ":registry/abc",
		"arn:ccp:bucket:par:" + t1 + ":bucket/b1/object/foo.txt",
		"arn:ccp:iam:::role/AdminAll",
		"arn:ccp:dbaas:abj:" + t1 + ":instance/pg/db1",
	}
	for _, a := range arns {
		t.Run(a, func(t *testing.T) {
			parts, err := ParseArn(a)
			if err != nil {
				t.Fatalf("ParseArn: %v", err)
			}
			rebuilt, err := BuildArn(parts.Service, parts.Region, parts.TenantID, parts.ResourcePath)
			if err != nil {
				t.Fatalf("BuildArn: %v", err)
			}
			if rebuilt != a {
				t.Errorf("roundtrip: got %q want %q", rebuilt, a)
			}
		})
	}
}

func TestArnPartsHelpers(t *testing.T) {
	p, _ := ParseArn("arn:ccp:registry:rnn:" + t1 + ":registry/foo")
	if p.IsBuiltIn() || p.IsWildcardTenant() {
		t.Errorf("regular tenant arn should not be built-in or wildcard")
	}
	bi, _ := ParseArn("arn:ccp:iam:::role/AdminAll")
	if !bi.IsBuiltIn() {
		t.Errorf("built-in not detected")
	}
	wc, _ := ParseArn("arn:ccp:registry:rnn:*:registry/foo")
	if !wc.IsWildcardTenant() {
		t.Errorf("wildcard tenant not detected")
	}
}

func TestMatchArn_BuiltInDoesNotMatchTenantSpecific(t *testing.T) {
	bi, _ := ArnForBuiltInRole("AdminAll")
	if MatchArn("arn:ccp:iam::"+t1+":role/AdminAll", bi) {
		t.Errorf("tenant-specific pattern should not match built-in")
	}
}

func TestMatchArn_UppercasePatternDoesNotNormalize(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if MatchArn("arn:ccp:registry:RNN:"+t1+":registry/abc", target) {
		t.Errorf("uppercase pattern should not match lowercase target (intentional)")
	}
}

func TestMatchArn_ActionStyleChars(t *testing.T) {
	target := "arn:ccp:registry:rnn:" + t1 + ":registry/abc"
	if !MatchArn("arn:ccp:*:*:"+t1+":*", target) {
		t.Errorf("wildcards on service+region+resource should match")
	}
}

func TestHelpersDbaasEngineInSubresource(t *testing.T) {
	pgArn, _ := ArnForDbaasInstance(t1, "rnn", "pg", "db1")
	valkeyArn, _ := ArnForDbaasInstance(t1, "rnn", "valkey", "db1")
	pgPattern := "arn:ccp:dbaas:rnn:" + t1 + ":instance/pg/*"
	if !MatchArn(pgPattern, pgArn) {
		t.Errorf("pg pattern should match pg arn")
	}
	if MatchArn(pgPattern, valkeyArn) {
		t.Errorf("pg pattern should not match valkey arn")
	}
}

func TestMatchArn_EmptySegments(t *testing.T) {
	target := "arn:ccp:iam:::role/Member"
	if !MatchArn("arn:ccp:iam:::role/Member", target) {
		t.Errorf("exact built-in match failed")
	}
	if !MatchArn("arn:ccp:iam:::role/*", target) {
		t.Errorf("wildcard role suffix should match")
	}
	if !MatchArn("arn:ccp:iam::*:*", target) {
		t.Errorf("wildcard tenant+resource should match empty tenant")
	}
}

// Extra coverage on UUID validator
func TestUUIDValidator(t *testing.T) {
	if !uuidRE.MatchString(t1) {
		t.Errorf("uuid t1 should match regex")
	}
	if uuidRE.MatchString("not-a-uuid") {
		t.Errorf("invalid uuid matched")
	}
}

// Extra coverage on glob matcher
func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"", "", true},
		{"*", "", true},
		{"*", "anything", true},
		{"a*b", "ab", true},
		{"a*b", "axyzb", true},
		{"a*b", "ac", false},
		{"a?b", "axb", true},
		{"a?b", "ab", false},
		{"a?b", "axyb", false},
		{"foo/*", "foo/bar/baz", true},
		{"*.txt", "a/b/c.txt", true},
		{"prefix*suffix", "prefix-middle-suffix", true},
	}
	for _, tc := range cases {
		if got := globMatch(tc.pat, tc.s); got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pat, tc.s, got, tc.want)
		}
	}
}
