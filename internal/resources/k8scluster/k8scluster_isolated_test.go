// Regression guard for isolated networks (#1325 / provider issue #64).
//
// A CCKS cluster may live in an ISOLATED subnet — one whose `snat` is `false`.
// The filter that once required internet egress dated from a time when the
// bootstrap needed it; node images are preloaded and name resolution is served
// from inside the network, so that is no longer the case, and a plan-time guard
// would now reject a configuration that is valid.
//
// The provider has no such guard today. This test is what stops one from coming
// back — the check is on the resource's actual behaviour, not on its source: a
// plan-time refusal has to live in one of the two hooks Terraform calls before
// apply, so a resource that implements neither cannot refuse a plan.
package k8scluster

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestNoPlanTimeGuardOnTheTargetSubnet(t *testing.T) {
	r := New()

	if _, ok := r.(resource.ResourceWithModifyPlan); ok {
		t.Error("ccp_k8s_cluster gained a ModifyPlan: make sure it does not reject a " +
			"vnet_id whose subnet is isolated (snat = false) — that is a valid configuration")
	}
	if _, ok := r.(resource.ResourceWithValidateConfig); ok {
		t.Error("ccp_k8s_cluster gained a ValidateConfig: make sure it does not reject a " +
			"vnet_id whose subnet is isolated (snat = false) — that is a valid configuration")
	}
	if _, ok := r.(resource.ResourceWithConfigValidators); ok {
		t.Error("ccp_k8s_cluster gained ConfigValidators: make sure none of them rejects a " +
			"vnet_id whose subnet is isolated (snat = false) — that is a valid configuration")
	}
}
