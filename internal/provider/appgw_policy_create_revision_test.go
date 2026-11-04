package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestGatewayCreateWaitsForItsFollowupPatchRevision(t *testing.T) {
	r, plan, fixture := policyHarness(t, "gateway")
	ctx := context.Background()
	noPolicyErrors(t, plan.SetAttribute(ctx, path.Root("custom_error_page_html"), types.StringValue("<html><body>Unavailable</body></html>")))
	var readsAfterPatch atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fixture.mu.Lock()
		afterPatch := len(fixture.writes) >= 2
		fixture.mu.Unlock()
		if afterPatch && req.Method == http.MethodGet && req.URL.Path == "/v1/app-gateways/gw" {
			reads := readsAfterPatch.Add(1)
			fixture.mu.Lock()
			generation := fixture.generation
			// The POST produced revision 6; PATCH produced revision 7. The
			// RO replica is still at revision 6 for the first two reads.
			if reads <= 2 {
				fixture.generation--
			}
			out := fixture.gateway()
			fixture.generation = generation
			fixture.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		fixture.serve(t, w, req)
	}))
	defer srv.Close()
	var configured resource.ConfigureResponse
	r.(resource.ResourceWithConfigure).Configure(ctx, resource.ConfigureRequest{ProviderData: client.New(srv.URL, "unit", "test")}, &configured)
	noPolicyErrors(t, configured.Diagnostics)
	created := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: plan.Raw, Schema: plan.Schema}, Config: tfsdk.Config{Raw: plan.Raw, Schema: plan.Schema}}, &created)
	noPolicyErrors(t, created.Diagnostics)
	var revision types.Int64
	noPolicyErrors(t, created.State.GetAttribute(ctx, path.Root("security_revision"), &revision))
	if revision.ValueInt64() != 7 || readsAfterPatch.Load() < 3 {
		t.Fatalf("followup PATCH reported applied before its revision: revision=%d reads=%d", revision.ValueInt64(), readsAfterPatch.Load())
	}
}
