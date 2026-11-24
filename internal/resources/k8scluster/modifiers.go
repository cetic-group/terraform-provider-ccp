package k8scluster

import (
	"context"
	"regexp"

	"github.com/cetic-group/terraform-provider-ccp/internal/ccks"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// versionFormat: `X.Y.Z` with an optional `v`. Anything else (`v1.35`) can
// neither be compared nor accepted by the API: refused at validation instead
// of a silent perpetual diff.
func versionFormat() validator.String {
	return stringvalidator.RegexMatches(regexp.MustCompile(ccks.VersionRegex), "must be a version X.Y.Z (e.g. v1.34.8)")
}

// platformTemplateKey is the form the platform writes into os_template_key on
// a version upgrade (`kube-v1-34-9`).
var platformTemplateKey = regexp.MustCompile(`^kube-v\d+-\d+-\d+$`)

// templateKeyRequiresReplace forces a replacement on an os_template_key
// change, EXCEPT between two keys of the platform form: the platform rewrites
// the key at every version upgrade, and a state refreshed by an earlier
// provider holds the rewritten key (`kube-v1-34-9` for a configured
// `kube-v1-34-8`) — replacing the cluster for it would destroy it. Such a
// change is planned in place and applied without any call (the configured
// value is recorded); the Kubernetes version is driven by `k8s_version`.
func templateKeyRequiresReplace() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !(platformTemplateKey.MatchString(req.StateValue.ValueString()) &&
				platformTemplateKey.MatchString(req.PlanValue.ValueString()))
		},
		"Forces replacement, except between two platform-written keys (kube-vX-Y-Z).",
		"Forces replacement, except between two platform-written keys (`kube-vX-Y-Z`).",
	)
}

// privateImportedPool marks a state whose initial_pool was rebuilt from the
// API at import (see Read): its disk_gb is the pool's actual size.
const privateImportedPool = "imported_initial_pool"

// diskReplaceUnlessImported forces a replacement on an initial_pool.disk_gb
// change, except when the configuration leaves it unset right after an
// import: the imported state holds the pool's actual size, the configuration
// none — not a change of disk.
func diskReplaceUnlessImported() planmodifier.Int64 {
	return int64planmodifier.RequiresReplaceIf(
		func(ctx context.Context, req planmodifier.Int64Request, resp *int64planmodifier.RequiresReplaceIfFuncResponse) {
			if req.ConfigValue.IsNull() && req.Private != nil {
				if v, d := req.Private.GetKey(ctx, privateImportedPool); !d.HasError() && len(v) > 0 {
					return
				}
			}
			resp.RequiresReplace = true
		},
		"Forces replacement (no resize), except when left unset right after an import.",
		"Forces replacement (no resize), except when left unset right after an import.",
	)
}
