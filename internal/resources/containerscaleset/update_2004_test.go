package containerscaleset

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func cssModele(ctx context.Context, tags []string, desired int64) cssResourceModel {
	l, _ := types.ListValueFrom(ctx, types.StringType, tags)
	keys, _ := types.ListValueFrom(ctx, types.StringType, []string{})
	return cssResourceModel{
		ID: types.StringValue("css-1"), Name: types.StringValue("web"), Region: types.StringValue("RNN"),
		Plan: types.StringValue("nano"), Template: types.StringValue("ubuntu-24.04"),
		VnetID: types.StringNull(), SSHKeyIDs: keys, UserData: types.StringNull(),
		RootPassword: types.StringValue("Secret123!"), MinInstances: types.Int64Value(1),
		MaxInstances: types.Int64Value(5), DesiredInstances: types.Int64Value(desired),
		AutoRepair: types.BoolValue(true), BastionAccess: types.BoolValue(false),
		Docker: types.BoolValue(false), DiskGB: types.Int64Null(), Status: types.StringValue("active"),
		Tags: l, CreatedAt: types.StringValue("2026-11-03T10:00:00Z"),
	}
}

func cssUpdate(t *testing.T, srv *testutil.Server, etat, voulu cssResourceModel) cssResourceModel {
	t.Helper()
	ctx := context.Background()
	var sr resource.SchemaResponse
	New().Schema(ctx, resource.SchemaRequest{}, &sr)
	s := sr.Schema
	r := &cssResource{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	state := tfsdk.State{Schema: s}
	state.Set(ctx, &etat)
	plan := tfsdk.Plan{Schema: s}
	plan.Set(ctx, &voulu)
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	r.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update : %v", resp.Diagnostics)
	}
	var final cssResourceModel
	resp.State.Get(ctx, &final)
	return final
}

func cssAPI(status string, tags []string, desired int) map[string]any {
	return map[string]any{
		"id": "css-1", "name": "web", "region": "RNN", "plan": "nano", "template": "ubuntu-24.04",
		"min_instances": 1, "max_instances": 5, "desired_instances": desired, "auto_repair": true,
		"status": status, "tags": tags, "created_at": "2026-11-03T10:00:00Z",
		"updated_at": "2026-11-03T10:00:00Z",
	}
}

// Revue de #121 : un changement de tags seul ne doit PAS envoyer de PATCH (même
// vide, il passe le scale set en `scaling` et relance un cycle). Le faux
// serveur n'a pas de route PATCH : un appel ferait échouer l'Update.
func TestTagsSeulsSansPatch(t *testing.T) {
	ctx := context.Background()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PUT", Path: "/v1/tags", Status: http.StatusOK, Body: map[string]any{}},
		{Method: "GET", Path: "/v1/container-scale-sets/css-1", Status: http.StatusOK,
			Body: cssAPI("active", []string{"a", "b"}, 2)},
	})
	defer srv.Close()
	final := cssUpdate(t, srv, cssModele(ctx, []string{"a"}, 2), cssModele(ctx, []string{"a", "b"}, 2))
	for _, c := range srv.Calls() {
		if c.Method == "PATCH" {
			t.Fatalf("PATCH inattendu : %s", c.Path)
		}
	}
	if final.RootPassword.ValueString() != "Secret123!" {
		t.Fatalf("root_password perdu : %v", final.RootPassword)
	}
}

// Un vrai changement (desired) envoie le PATCH ; l'API répond `scaling`, mais
// l'état garde le `status` planifié et `root_password` (sinon « inconsistent
// result after apply »).
func TestScalingGardeStatusEtRootPassword(t *testing.T) {
	ctx := context.Background()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "PATCH", Path: "/v1/container-scale-sets/css-1", Status: http.StatusOK,
			Body: cssAPI("scaling", []string{"a"}, 3)},
	})
	defer srv.Close()
	final := cssUpdate(t, srv, cssModele(ctx, []string{"a"}, 2), cssModele(ctx, []string{"a"}, 3))
	if final.Status.ValueString() != "active" {
		t.Fatalf("status = %v, attendu le status planifié", final.Status)
	}
	if final.RootPassword.ValueString() != "Secret123!" {
		t.Fatalf("root_password perdu : %v", final.RootPassword)
	}
	if final.DesiredInstances.ValueInt64() != 3 {
		t.Fatalf("desired = %v", final.DesiredInstances)
	}
}
