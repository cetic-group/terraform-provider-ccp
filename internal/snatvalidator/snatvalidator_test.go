// Tests for the isolated-subnet notice (#67).
//
// The behaviour under test is a DIAGNOSTIC SEVERITY, and severity is the whole
// point: the previous version raised a hard error, which closed a supported
// configuration — a subnet with `snat = false` where the startup script needs
// no internet at all — and left no way around it.
package snatvalidator

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

const reason = "This instance carries a `user_data` script."

func vnetRoutes(snat bool) testutil.Routes {
	return testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs", Status: http.StatusOK,
			Body: []map[string]any{{"id": "vpc-1", "name": "corp", "region": "RNN"}}},
		{Method: "GET", Path: "/v1/vpcs/vpc-1/vnets", Status: http.StatusOK,
			Body: []map[string]any{{
				"id": "vn-1", "vpc_id": "vpc-1", "name": "airgap-workers",
				"cidr": "10.30.0.0/24", "snat": snat, "isolated": !snat,
				"status": "active", "created_at": "2026-09-01T10:00:00Z",
			}}},
	}
}

func run(t *testing.T, routes testutil.Routes, vnetID string) diag.Diagnostics {
	t.Helper()
	srv := testutil.NewTestServer(t, routes)
	defer srv.Close()
	var diags diag.Diagnostics
	CheckVnetSnat(context.Background(), client.New(srv.URL, "ccp_test_unit", "0.0.0-test"),
		vnetID, reason, &diags)
	return diags
}

// The core of #67: an isolated subnet is a supported configuration. Refusing it
// closed valid work — a script that writes a file or starts software already in
// the image needs no internet, and the check never looks at the script.
func TestIsolatedSubnetWarnsAndDoesNotBlock(t *testing.T) {
	diags := run(t, vnetRoutes(false), "vn-1")

	if diags.HasError() {
		t.Fatalf("an isolated subnet must not fail the plan: %v", diags.Errors())
	}
	if diags.WarningsCount() != 1 {
		t.Fatalf("expected exactly one warning, got %d", diags.WarningsCount())
	}
	w := diags.Warnings()[0]
	if !strings.Contains(w.Detail(), "airgap-workers") {
		t.Errorf("the notice must name the subnet: %s", w.Detail())
	}
	// It has to say what to do, and that the failure will not appear here.
	if !strings.Contains(w.Detail(), "snat = true") {
		t.Errorf("the notice must name the way out: %s", w.Detail())
	}
	if !strings.Contains(w.Detail(), reason) {
		t.Errorf("the caller's reason must be relayed: %s", w.Detail())
	}
}

func TestSubnetWithEgressSaysNothing(t *testing.T) {
	diags := run(t, vnetRoutes(true), "vn-1")
	if len(diags) != 0 {
		t.Fatalf("a subnet with internet access must produce no diagnostic: %v", diags)
	}
}

// A probe failure says nothing about the subnet, so it must stay silent —
// warning on every network blip would cry wolf.
//
// This silence is also WHY the verdict is a warning and not an error: a gate
// that skips whenever its own probe fails blocks only whoever it can reach, and
// is bypassed by breaking the probe. A warning that is occasionally absent is
// honest; a hard error that is occasionally absent is not a gate at all.
func TestProbeFailureStaysSilent(t *testing.T) {
	diags := run(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs", Status: http.StatusInternalServerError,
			Body: map[string]any{"detail": "upstream down"}},
	}, "vn-1")
	if len(diags) != 0 {
		t.Fatalf("a probe failure must produce no diagnostic: %v", diags)
	}
}

func TestUnknownSubnetStaysSilent(t *testing.T) {
	// The VNet is not in any VPC: apply will report that far more clearly.
	diags := run(t, vnetRoutes(false), "vn-does-not-exist")
	if len(diags) != 0 {
		t.Fatalf("an unresolvable subnet must produce no diagnostic: %v", diags)
	}
}

func TestNoClientOrNoVnetIsANoOp(t *testing.T) {
	var diags diag.Diagnostics
	CheckVnetSnat(context.Background(), nil, "vn-1", reason, &diags)
	if len(diags) != 0 {
		t.Fatalf("no client configured yet: must be a no-op, got %v", diags)
	}
	srv := testutil.NewTestServer(t, testutil.Routes{})
	defer srv.Close()
	CheckVnetSnat(context.Background(), client.New(srv.URL, "ccp_test_unit", "0.0.0-test"), "", reason, &diags)
	if len(diags) != 0 {
		t.Fatalf("no vnet_id in the plan: must be a no-op, got %v", diags)
	}
}
