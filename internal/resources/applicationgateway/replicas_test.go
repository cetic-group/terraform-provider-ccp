package applicationgateway

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	"testing"
)

func TestCreateSingleInstance(t *testing.T) {
	plan := planModel(types.StringUnknown())
	plan.VIPAddress = types.StringUnknown()
	plan.ReplicaCount = types.Int64Value(1)
	sent, state, diags := runCreateDiags(t, plan, "10.0.1.254", "10.0.1.254", 1)
	if diags.HasError() {
		t.Fatalf("create: %v", diags)
	}
	if sent["replica_count"] != float64(1) || state.ReplicaCount.ValueInt64() != 1 {
		t.Fatalf("single mode not sent/read: %v %v", sent, state.ReplicaCount)
	}
}

func TestCreateSingleLegacyServerKeepsIDAndFails(t *testing.T) {
	plan := planModel(types.StringUnknown())
	plan.VIPAddress = types.StringUnknown()
	plan.ReplicaCount = types.Int64Value(1)
	_, state, diags := runCreateDiags(t, plan, "10.0.1.254", "10.0.1.254")
	if !diags.HasError() {
		t.Fatal("expected unsupported replica count diagnostic")
	}
	if state.ID.ValueString() == "" || state.ReplicaCount.ValueInt64() != 2 {
		t.Fatalf("created object must stay in state with its actual historical mode: %+v", state)
	}
}

func TestCreateHALegacyServerCompatible(t *testing.T) {
	plan := planModel(types.StringUnknown())
	plan.VIPAddress = types.StringUnknown()
	plan.ReplicaCount = types.Int64Value(2)
	_, state, diags := runCreateDiags(t, plan, "10.0.1.254", "10.0.1.254")
	if diags.HasError() || state.ReplicaCount.ValueInt64() != 2 {
		t.Fatalf("legacy HA: %v %v", diags, state.ReplicaCount)
	}
}
