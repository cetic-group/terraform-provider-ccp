// Package iamarn implements the CETIC Cloud Platform ARN scheme in Go.
//
// Grammar:
//
//	arn:ccp:<service>:<region>:<tenant_id>:<resource_path>
//
//   - 6 segments separated by `:`. Prefix is always `arn:ccp`.
//   - service ∈ iam | registry | bucket | k8s | vm | container | vpc | lb |
//     publicip | volume | dbaas | billing | support | org | * (wildcard).
//   - region ∈ rnn | par | abj | "" (global) | * (wildcard).
//   - tenant_id: UUID v4 OR `*`. Empty = built-in (global roles only).
//   - resource_path: `<resource_type>/<resource_id>[/<sub_type>/<sub_id>]*`,
//     glob libre fnmatch (`*`, `?`).
//
// Double match semantics:
//   - First 5 segments (arn, ccp, service, region, tenant_id) → strict
//     segment-by-segment, with `*`/`?` wildcards allowed within each segment.
//   - `resource_path` (everything after the 5th `:`) → free fnmatch glob over
//     the whole string (`*` traverses `/`).
//
// This is a Go port of `apps/api/app/services/iam_arn.py`. Behaviour parity is
// validated by `iamarn_test.go` which ports the 75 cases from
// `apps/api/tests/test_iam_arn.py`.
package iamarn

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// knownServices is the whitelist of services (cf. iam_catalog.SERVICES).
var knownServices = map[string]struct{}{
	"iam": {}, "registry": {}, "bucket": {}, "k8s": {}, "vm": {},
	"container": {}, "vpc": {}, "lb": {}, "publicip": {}, "volume": {},
	"dbaas": {}, "billing": {}, "support": {}, "org": {},
}

// knownRegions is the whitelist of regions (lowercase) plus empty.
var knownRegions = map[string]struct{}{
	"rnn": {}, "par": {}, "abj": {}, "": {},
}

// uuidRE matches a v4-ish UUID hexadecimal form (case-insensitive).
var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ArnParts is the decomposition of an ARN.
//
// TenantID is kept as a string ("" for built-in, "*" for wildcard, UUID
// hex otherwise). Conversion to uuid.UUID is left to the caller because
// some functions must handle the wildcard.
type ArnParts struct {
	Service      string
	Region       string
	TenantID     string
	ResourcePath string
}

// IsWildcardTenant reports whether TenantID == "*".
func (a ArnParts) IsWildcardTenant() bool { return a.TenantID == "*" }

// IsBuiltIn reports whether TenantID == "" (built-in role ARN).
func (a ArnParts) IsBuiltIn() bool { return a.TenantID == "" }

// ─── Build / parse ──────────────────────────────────────────────────────────

// BuildArn constructs an ARN from its components.
//
// Does not validate semantics (a caller can build an ARN with `*` everywhere).
// Returns an error if a segment contains `:`.
func BuildArn(service, region, tenantID, resourcePath string) (string, error) {
	regionNorm := strings.ToLower(strings.TrimSpace(region))

	for _, kv := range []struct{ label, val string }{
		{"service", service},
		{"region", regionNorm},
		{"tenant_id", tenantID},
		{"resource_path", resourcePath},
	} {
		if strings.Contains(kv.val, ":") {
			return "", fmt.Errorf("build_arn: segment `%s` cannot contain `:` (got %q)", kv.label, kv.val)
		}
	}

	return fmt.Sprintf("arn:ccp:%s:%s:%s:%s", service, regionNorm, tenantID, resourcePath), nil
}

// ErrInvalidArn is returned by ParseArn for malformed ARNs.
var ErrInvalidArn = errors.New("invalid arn")

// ParseArn parses a strict ARN. Returns an error if malformed.
//
// Accepts `*`/`?` wildcards within each segment (useful for policy
// document patterns). The global wildcard `"*"` (which matches everything)
// is acceptable as a *matching pattern* but is NOT a valid ARN; it is
// handled specially in MatchArn.
func ParseArn(arn string) (ArnParts, error) {
	if arn == "" {
		return ArnParts{}, fmt.Errorf("%w: empty string", ErrInvalidArn)
	}

	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return ArnParts{}, fmt.Errorf("%w: format invalid — expected 6 `:`-separated segments (got %q)", ErrInvalidArn, arn)
	}

	prefix, ns, service, region, tenantID, resourcePath := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]

	if prefix != "arn" {
		return ArnParts{}, fmt.Errorf("%w: must start with `arn:` (got prefix %q)", ErrInvalidArn, prefix)
	}
	if ns != "ccp" {
		return ArnParts{}, fmt.Errorf("%w: namespace must be `ccp` (got %q)", ErrInvalidArn, ns)
	}
	if service == "" {
		return ArnParts{}, fmt.Errorf("%w: `service` cannot be empty", ErrInvalidArn)
	}
	if resourcePath == "" {
		return ArnParts{}, fmt.Errorf("%w: `resource_path` cannot be empty", ErrInvalidArn)
	}

	if !isSegmentValidCharset(service) {
		return ArnParts{}, fmt.Errorf("%w: `service` contains invalid characters (got %q)", ErrInvalidArn, service)
	}
	if !strings.ContainsAny(service, "*?") {
		if _, ok := knownServices[service]; !ok {
			return ArnParts{}, fmt.Errorf("%w: unknown `service` (got %q)", ErrInvalidArn, service)
		}
	}

	if !isSegmentValidCharset(region) {
		return ArnParts{}, fmt.Errorf("%w: `region` contains invalid characters (got %q)", ErrInvalidArn, region)
	}
	regionNorm := strings.ToLower(region)
	if !strings.ContainsAny(regionNorm, "*?") {
		if _, ok := knownRegions[regionNorm]; !ok {
			return ArnParts{}, fmt.Errorf("%w: unknown `region` (got %q)", ErrInvalidArn, region)
		}
	}

	if !isSegmentValidCharset(tenantID) {
		return ArnParts{}, fmt.Errorf("%w: `tenant_id` contains invalid characters (got %q)", ErrInvalidArn, tenantID)
	}
	if tenantID != "" && !strings.ContainsAny(tenantID, "*?") {
		if !uuidRE.MatchString(tenantID) {
			return ArnParts{}, fmt.Errorf("%w: `tenant_id` must be UUID, empty, or wildcard (got %q)", ErrInvalidArn, tenantID)
		}
	}

	if !isSegmentValidCharset(resourcePath) {
		return ArnParts{}, fmt.Errorf("%w: `resource_path` contains invalid characters (got %q)", ErrInvalidArn, resourcePath)
	}

	return ArnParts{
		Service:      service,
		Region:       regionNorm,
		TenantID:     tenantID,
		ResourcePath: resourcePath,
	}, nil
}

// isSegmentValidCharset reports whether segment contains only allowed chars.
// Allowed: alphanumerics, `-`, `_`, `.`, `*`, `?`, `/`.
func isSegmentValidCharset(segment string) bool {
	if segment == "" {
		return true
	}
	for _, c := range segment {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			continue
		case c == '-' || c == '_' || c == '.' || c == '*' || c == '?' || c == '/':
			continue
		default:
			return false
		}
	}
	return true
}

// ─── Matching ───────────────────────────────────────────────────────────────

// MatchArn reports whether target (a concrete ARN) matches pattern (an ARN
// possibly containing wildcards).
//
// Semantics:
//   - pattern == "*" matches every target (cf. PolicyStatement with
//     resources: ["*"]).
//   - Otherwise, the 6 segments are matched segment-by-segment, with `*`/`?`
//     wildcards allowed within each segment. For the 6th segment
//     (resource_path), free fnmatch is allowed — `*` can traverse `/`.
//
// Returns false if either argument is malformed (no panic).
func MatchArn(pattern, target string) bool {
	if pattern == "*" {
		return true
	}

	targetParts := strings.SplitN(target, ":", 6)
	if len(targetParts) < 6 {
		return false
	}
	if targetParts[0] != "arn" || targetParts[1] != "ccp" {
		return false
	}

	patternParts := strings.SplitN(pattern, ":", 6)
	if len(patternParts) < 6 {
		return false
	}
	if patternParts[0] != "arn" || patternParts[1] != "ccp" {
		return false
	}

	// Segments 2..4 (service, region, tenant_id): strict per-segment fnmatch.
	for idx := 2; idx <= 4; idx++ {
		if !fnmatchCase(targetParts[idx], patternParts[idx]) {
			return false
		}
	}

	// Segment 5 (resource_path): free fnmatch over the whole string.
	return fnmatchCase(targetParts[5], patternParts[5])
}

// MatchesAny reports whether at least one pattern matches target.
func MatchesAny(patterns []string, target string) bool {
	for _, p := range patterns {
		if MatchArn(p, target) {
			return true
		}
	}
	return false
}

// fnmatchCase performs a case-sensitive fnmatch. We rely on filepath.Match
// for `*`/`?` semantics, which match Python's fnmatch.fnmatchcase exactly
// for our charset.
//
// filepath.Match's `*` does traverse `/` on its own (Go's filepath.Match
// treats `*` as "matches any sequence of non-Separator characters" only on
// Windows; on Unix the separator is `/`).
//
// To keep behaviour identical across OSes and to ensure `*` traverses `/`
// (required by IAM ARN resource_path semantics), we implement a small
// glob matcher manually.
func fnmatchCase(s, pattern string) bool {
	return globMatch(pattern, s)
}

// globMatch is a small recursive glob matcher with support for `*` (any
// sequence of chars including `/`) and `?` (exactly one char including `/`).
// Behaviour matches Python's fnmatch.fnmatchcase for our use case.
func globMatch(pattern, s string) bool {
	// Iterative implementation with backtrack pointers — O(len(pattern)+len(s))
	// in the typical case, falls back to backtracking on consecutive `*`.
	pi, si := 0, 0
	starPi, starSi := -1, 0
	for si < len(s) {
		if pi < len(pattern) {
			c := pattern[pi]
			switch c {
			case '?':
				pi++
				si++
				continue
			case '*':
				starPi = pi
				starSi = si
				pi++
				continue
			default:
				if pattern[pi] == s[si] {
					pi++
					si++
					continue
				}
			}
		}
		if starPi != -1 {
			pi = starPi + 1
			starSi++
			si = starSi
			continue
		}
		return false
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// ─── Helpers (resource_path par type de ressource) ──────────────────────────

// ArnForRegistry builds an ARN for a registry resource.
func ArnForRegistry(tenantID, region, registryID string) (string, error) {
	return BuildArn("registry", region, tenantID, "registry/"+registryID)
}

// ArnForRepository builds an ARN for a repository within a registry.
func ArnForRepository(tenantID, region, registryID, repo string) (string, error) {
	return BuildArn("registry", region, tenantID, "registry/"+registryID+"/repository/"+repo)
}

// ArnForBucket builds an ARN for a bucket.
func ArnForBucket(tenantID, region, bucketID string) (string, error) {
	return BuildArn("bucket", region, tenantID, "bucket/"+bucketID)
}

// ArnForObject builds an ARN for an object inside a bucket.
func ArnForObject(tenantID, region, bucketID, objectKey string) (string, error) {
	return BuildArn("bucket", region, tenantID, "bucket/"+bucketID+"/object/"+objectKey)
}

// ArnForK8sCluster builds an ARN for a K8s cluster.
func ArnForK8sCluster(tenantID, region, clusterID string) (string, error) {
	return BuildArn("k8s", region, tenantID, "cluster/"+clusterID)
}

// ArnForVM builds an ARN for a VM instance.
func ArnForVM(tenantID, region, vmID string) (string, error) {
	return BuildArn("vm", region, tenantID, "vm/"+vmID)
}

// ArnForContainer builds an ARN for a container instance.
func ArnForContainer(tenantID, region, containerID string) (string, error) {
	return BuildArn("container", region, tenantID, "container/"+containerID)
}

// ArnForVPC builds an ARN for a VPC.
func ArnForVPC(tenantID, region, vpcID string) (string, error) {
	return BuildArn("vpc", region, tenantID, "vpc/"+vpcID)
}

// ArnForLB builds an ARN for a load balancer.
func ArnForLB(tenantID, region, lbID string) (string, error) {
	return BuildArn("lb", region, tenantID, "lb/"+lbID)
}

// ArnForPublicIP builds an ARN for a public IP.
func ArnForPublicIP(tenantID, region, ipID string) (string, error) {
	return BuildArn("publicip", region, tenantID, "ip/"+ipID)
}

// ArnForVolume builds an ARN for a block volume.
func ArnForVolume(tenantID, region, volumeID string) (string, error) {
	return BuildArn("volume", region, tenantID, "volume/"+volumeID)
}

// ArnForDbaasInstance builds an ARN for a DBaaS instance. The engine
// (pg | valkey | mariadb | ferretdb) is the sub-resource discriminator.
func ArnForDbaasInstance(tenantID, region, engine, instanceID string) (string, error) {
	return BuildArn("dbaas", region, tenantID, "instance/"+engine+"/"+instanceID)
}

// ArnForBilling builds an ARN for billing (region empty/global).
func ArnForBilling(tenantID, suffix string) (string, error) {
	if suffix == "" {
		suffix = "*"
	}
	return BuildArn("billing", "", tenantID, suffix)
}

// ArnForRole builds an ARN for a custom IAM role (region empty, tenant scoped).
func ArnForRole(tenantID, roleNameOrID string) (string, error) {
	return BuildArn("iam", "", tenantID, "role/"+roleNameOrID)
}

// ArnForBuiltInRole builds an ARN for a built-in role (tenant_id empty, global).
func ArnForBuiltInRole(roleName string) (string, error) {
	return BuildArn("iam", "", "", "role/"+roleName)
}

// ArnForServiceAccount builds an ARN for a service account.
func ArnForServiceAccount(tenantID, saID string) (string, error) {
	return BuildArn("iam", "", tenantID, "service-account/"+saID)
}
