package planmodifier

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func etatPresent() tfsdk.State {
	return tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})}
}

func TestCreateOnlyStringGardeLEtatEtAvertit(t *testing.T) {
	req := planmodifier.StringRequest{
		State: etatPresent(), StateValue: types.StringValue("3.0"),
		ConfigValue: types.StringValue("2.8"), PlanValue: types.StringValue("2.8"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	CreateOnlyString("image_tag").PlanModifyString(context.Background(), req, resp)
	if resp.PlanValue.ValueString() != "3.0" {
		t.Fatalf("la valeur de l'état doit rester au plan, reçu %v", resp.PlanValue)
	}
	if resp.Diagnostics.WarningsCount() != 1 || resp.Diagnostics.HasError() {
		t.Fatalf("un avertissement attendu : %v", resp.Diagnostics)
	}
}

func TestCreateOnlyStringLaisseLaCreation(t *testing.T) {
	req := planmodifier.StringRequest{
		State: tfsdk.State{Raw: tftypes.NewValue(tftypes.Object{}, nil)}, StateValue: types.StringNull(),
		ConfigValue: types.StringValue("2.8"), PlanValue: types.StringValue("2.8"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	CreateOnlyString("image_tag").PlanModifyString(context.Background(), req, resp)
	if resp.PlanValue.ValueString() != "2.8" {
		t.Fatalf("à la création, la valeur configurée part : %v", resp.PlanValue)
	}
}

func TestTimeWithinToleranceSupprimeLEcart(t *testing.T) {
	req := planmodifier.StringRequest{
		StateValue:  types.StringValue("2027-01-16T09:31:07Z"),
		ConfigValue: types.StringValue("2027-01-15T12:00:00Z"),
		PlanValue:   types.StringValue("2027-01-15T12:00:00Z"),
	}
	resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
	TimeWithinTolerance(48*time.Hour).PlanModifyString(context.Background(), req, resp)
	if resp.PlanValue.ValueString() != "2027-01-16T09:31:07Z" {
		t.Fatalf("écart < 48 h : la valeur de l'état doit rester au plan, reçu %v", resp.PlanValue)
	}
	req.ConfigValue = types.StringValue("2027-03-01T00:00:00Z")
	resp = &planmodifier.StringResponse{PlanValue: req.ConfigValue}
	TimeWithinTolerance(48*time.Hour).PlanModifyString(context.Background(), req, resp)
	if resp.PlanValue.ValueString() != "2027-03-01T00:00:00Z" {
		t.Fatalf("écart réel : la valeur configurée doit rester, reçu %v", resp.PlanValue)
	}
}
