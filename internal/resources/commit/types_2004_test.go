package commit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// cetic-cloud-platform#2004 : l'API accepte aussi two_years et three_years.
func TestCommitTypeAccepteLesDureesDeLAPI(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr := resp.Schema.Attributes["commit_type"].(schema.StringAttribute)
	for _, v := range []string{"monthly", "yearly", "two_years", "three_years"} {
		var r validator.StringResponse
		for _, val := range attr.Validators {
			val.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(v)}, &r)
		}
		if r.Diagnostics.HasError() {
			t.Errorf("%s refusé : %v", v, r.Diagnostics)
		}
	}
}

// Revue de #121 : un changement d'auto_renew est refusé dès le plan.
func TestAutoRenewRefuseAuPlan(t *testing.T) {
	ctx := context.Background()
	var sresp resource.SchemaResponse
	New().Schema(ctx, resource.SchemaRequest{}, &sresp)
	base := commitModel{
		ID: types.StringValue("c1"), TenantID: types.StringValue("t1"),
		CommitType: types.StringValue("monthly"), DiscountPct: types.Int64Value(10),
		StartAt: types.StringValue("2026-11-01T00:00:00Z"), EndAt: types.StringValue("2026-12-01T00:00:00Z"),
		AutoRenew: types.BoolValue(true), CanceledAt: types.StringNull(),
	}
	state := tfsdk.State{Schema: sresp.Schema}
	state.Set(ctx, &base)
	voulu := base
	voulu.AutoRenew = types.BoolValue(false)
	plan := tfsdk.Plan{Schema: sresp.Schema}
	plan.Set(ctx, &voulu)
	resp := &resource.ModifyPlanResponse{Plan: plan}
	New().(resource.ResourceWithModifyPlan).ModifyPlan(ctx, resource.ModifyPlanRequest{State: state, Plan: plan}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("le changement d'auto_renew doit être refusé au plan")
	}
	plan.Set(ctx, &base)
	resp = &resource.ModifyPlanResponse{Plan: plan}
	New().(resource.ResourceWithModifyPlan).ModifyPlan(ctx, resource.ModifyPlanRequest{State: state, Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("aucun changement : pas d'erreur attendue, reçu %v", resp.Diagnostics)
	}
}

// Revue de #121 : changer commit_type (remplacement) ET auto_renew dans le
// même plan crée un nouvel engagement avec le nouvel auto_renew — pas
// d'erreur au plan.
func TestAutoRenewAccepteAvecUnRemplacement(t *testing.T) {
	ctx := context.Background()
	var sresp resource.SchemaResponse
	New().Schema(ctx, resource.SchemaRequest{}, &sresp)
	base := commitModel{
		ID: types.StringValue("c1"), TenantID: types.StringValue("t1"),
		CommitType: types.StringValue("yearly"), DiscountPct: types.Int64Value(10),
		StartAt: types.StringValue("2026-11-01T00:00:00Z"), EndAt: types.StringValue("2027-11-01T00:00:00Z"),
		AutoRenew: types.BoolValue(true), CanceledAt: types.StringNull(),
	}
	state := tfsdk.State{Schema: sresp.Schema}
	state.Set(ctx, &base)
	voulu := base
	voulu.CommitType = types.StringValue("three_years")
	voulu.AutoRenew = types.BoolValue(false)
	plan := tfsdk.Plan{Schema: sresp.Schema}
	plan.Set(ctx, &voulu)
	resp := &resource.ModifyPlanResponse{Plan: plan}
	New().(resource.ResourceWithModifyPlan).ModifyPlan(ctx, resource.ModifyPlanRequest{State: state, Plan: plan}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("remplacement prévu : auto_renew doit être accepté, reçu %v", resp.Diagnostics)
	}
}
