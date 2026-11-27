package vnetfirewallrule

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// cetic-cloud-platform#2004 : pas de PATCH pour une règle côté API ; comment,
// position et enabled doivent recréer la règle au lieu d'appeler une route
// inexistante.
func TestAttributsModifiablesForcentLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	descs := map[string][]string{}
	for nom, a := range resp.Schema.Attributes {
		switch v := a.(type) {
		case schema.StringAttribute:
			for _, m := range v.PlanModifiers {
				descs[nom] = append(descs[nom], m.Description(context.Background()))
			}
		case schema.Int64Attribute:
			for _, m := range v.PlanModifiers {
				descs[nom] = append(descs[nom], m.Description(context.Background()))
			}
		case schema.BoolAttribute:
			for _, m := range v.PlanModifiers {
				descs[nom] = append(descs[nom], m.Description(context.Background()))
			}
		}
	}
	for _, nom := range []string{"comment", "enabled"} {
		if !strings.Contains(strings.Join(descs[nom], " "), "recreate") {
			t.Errorf("%s doit forcer le remplacement", nom)
		}
	}
	// `position` : fixée à la création, jamais recréée (le réordonnancement de
	// la console renumérote toutes les règles, revue de #121).
	if strings.Contains(strings.Join(descs["position"], " "), "recreate") {
		t.Error("position ne doit pas forcer le remplacement")
	}
}

// Revue de #121 : un remplacement (ici un changement de commentaire) réapplique
// la position configurée ; si elle diffère de la position actuelle, le plan
// l'annonce par un avertissement.
func TestRemplacementQuiChangeLaPositionEstAnnonce(t *testing.T) {
	ctx := context.Background()
	var sresp resource.SchemaResponse
	New().Schema(ctx, resource.SchemaRequest{}, &sresp)
	etat := fwModel{
		ID: types.StringValue("r1"), VnetID: types.StringValue("v1"), Direction: types.StringValue("in"),
		Action: types.StringValue("ACCEPT"), Proto: types.StringValue("tcp"), SourceCIDR: types.StringNull(),
		DestCIDR: types.StringNull(), Dport: types.StringValue("22"), Comment: types.StringValue("ssh"),
		Position: types.Int64Value(30), Enabled: types.BoolValue(true), CreatedAt: types.StringValue("x"),
	}
	cas := []struct {
		nom              string
		comment          string
		positionConfig   types.Int64
		avertissementVou bool
	}{
		{"commentaire changé, position 1 configurée", "ssh admin", types.Int64Value(1), true},
		{"commentaire changé, position omise", "ssh admin", types.Int64Null(), true},
		{"commentaire changé, position actuelle configurée", "ssh admin", types.Int64Value(30), false},
		{"aucun remplacement (position seule différente)", "ssh", types.Int64Value(1), false},
	}
	for _, c := range cas {
		state := tfsdk.State{Schema: sresp.Schema}
		state.Set(ctx, &etat)
		voulu := etat
		voulu.Comment = types.StringValue(c.comment)
		plan := tfsdk.Plan{Schema: sresp.Schema}
		plan.Set(ctx, &voulu)
		conf := voulu
		conf.Position = c.positionConfig
		conf.ID, conf.CreatedAt = types.StringNull(), types.StringNull()
		config := tfsdk.Config{Schema: sresp.Schema}
		config.Raw = mustRaw(t, ctx, sresp.Schema, &conf)
		resp := &resource.ModifyPlanResponse{Plan: plan}
		New().(resource.ResourceWithModifyPlan).ModifyPlan(ctx,
			resource.ModifyPlanRequest{State: state, Plan: plan, Config: config}, resp)
		if got := resp.Diagnostics.WarningsCount() > 0; got != c.avertissementVou {
			t.Errorf("%s : avertissement=%v, attendu %v (%v)", c.nom, got, c.avertissementVou, resp.Diagnostics)
		}
	}
}

func mustRaw(t *testing.T, ctx context.Context, s schema.Schema, m *fwModel) tftypes.Value {
	t.Helper()
	p := tfsdk.Plan{Schema: s}
	if d := p.Set(ctx, m); d.HasError() {
		t.Fatal(d)
	}
	return p.Raw
}
