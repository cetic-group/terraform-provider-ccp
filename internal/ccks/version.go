// Package ccks hosts what ccp_k8s_cluster and ccp_k8s_node_pool share about
// the Kubernetes versions of a CCKS cluster (cetic-cloud-platform#2143):
// version comparison, the "never downgrade" read-back rule, and the waits
// on the upgrade operations of the platform.
package ccks

import (
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Normalize strips the `v` prefix and the surrounding spaces, like the API
// (`v1.34.8` → `1.34.8`).
func Normalize(v string) string {
	return strings.TrimLeft(strings.TrimSpace(v), "vV")
}

// Parse reads `X.Y.Z` (with or without `v`). ok is false otherwise.
func Parse(v string) (out [3]int, ok bool) {
	parts := strings.Split(Normalize(v), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Compare returns -1, 0 or 1 as a is below, equal to or above b. ok is false
// when either is not an `X.Y.Z` version.
func Compare(a, b string) (cmp int, ok bool) {
	pa, oka := Parse(a)
	pb, okb := Parse(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1, true
		case pa[i] > pb[i]:
			return 1, true
		}
	}
	return 0, true
}

// IsAbove reports whether target is a strictly higher version than current
// (false when either cannot be parsed: the caller then lets the API decide).
func IsAbove(target, current string) bool {
	c, ok := Compare(target, current)
	return ok && c > 0
}

// Floor is the read-back rule of a desired Kubernetes version.
//
// The platform moves versions UP on its own: automatic patches inside the
// maintenance window, upgrades from the console, pools pinned on an explicit
// version when the control plane is upgraded. It never moves them down, and
// the provider never downgrades either. So the desired version kept in the
// state is the prior one (the configuration, written as the user wrote it,
// `v` prefix included) as long as the actual version is at or above it — no
// perpetual diff, no attempt to roll a patched cluster back. The actual
// version is exposed separately (`current_k8s_version`).
//
// Without a usable prior value (creation without a version, import) or when
// the actual version is below the prior one, the actual version wins.
func Floor(prior types.String, actual *string) types.String {
	if actual == nil || *actual == "" {
		if prior.IsUnknown() {
			return types.StringNull()
		}
		return prior
	}
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() != "" {
		if c, ok := Compare(*actual, prior.ValueString()); ok && c >= 0 {
			return prior
		}
	}
	return types.StringValue(*actual)
}
