package loadbalancer

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Blocs encore inconnus : `dynamic "backend"` sur une variable au validate, ou
// sur des ressources créées dans le même apply. Décoder la config dans des
// slices Go échouait (« target type cannot handle unknown values ») — sur
// TOUTE config de ce genre, dès la montée de version.

func lbObjectType(t *testing.T) (resource.SchemaResponse, tftypes.Object) {
	t.Helper()
	var sch resource.SchemaResponse
	(&lbResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sch)
	return sch, sch.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
}

// nullsExcept rend un objet dont chaque attribut vaut null, sauf ceux fournis.
func nullsExcept(ot tftypes.Object, vals map[string]tftypes.Value) tftypes.Value {
	out := make(map[string]tftypes.Value, len(ot.AttributeTypes))
	for k, ty := range ot.AttributeTypes {
		if v, ok := vals[k]; ok {
			out[k] = v
			continue
		}
		out[k] = tftypes.NewValue(ty, nil)
	}
	return tftypes.NewValue(ot, out)
}

func lbWithListeners(t *testing.T, listeners func(lt tftypes.List) tftypes.Value) (resource.SchemaResponse, tftypes.Value) {
	sch, ot := lbObjectType(t)
	lt := ot.AttributeTypes["listener"].(tftypes.List)
	return sch, nullsExcept(ot, map[string]tftypes.Value{
		"name":     tftypes.NewValue(tftypes.String, "web"),
		"listener": listeners(lt),
	})
}

func listenerWithBackend(lt tftypes.List, backend tftypes.Value) tftypes.Value {
	return nullsExcept(lt.ElementType.(tftypes.Object), map[string]tftypes.Value{
		"protocol":    tftypes.NewValue(tftypes.String, "http"),
		"listen_port": tftypes.NewValue(tftypes.Number, 80),
		"backend":     backend,
	})
}

func backendType(lt tftypes.List) tftypes.List {
	return lt.ElementType.(tftypes.Object).AttributeTypes["backend"].(tftypes.List)
}

func TestValidateConfig_UnknownBlocksDoNotFail(t *testing.T) {
	cases := map[string]func(lt tftypes.List) tftypes.Value{
		"listener inconnu": func(lt tftypes.List) tftypes.Value {
			return tftypes.NewValue(lt, tftypes.UnknownValue)
		},
		"backend inconnu": func(lt tftypes.List) tftypes.Value {
			return tftypes.NewValue(lt, []tftypes.Value{
				listenerWithBackend(lt, tftypes.NewValue(backendType(lt), tftypes.UnknownValue)),
			})
		},
		"un backend inconnu dans la liste": func(lt tftypes.List) tftypes.Value {
			bt := backendType(lt)
			return tftypes.NewValue(lt, []tftypes.Value{
				listenerWithBackend(lt, tftypes.NewValue(bt, []tftypes.Value{
					tftypes.NewValue(bt.ElementType, tftypes.UnknownValue),
				})),
			})
		},
	}
	for name, listeners := range cases {
		t.Run(name, func(t *testing.T) {
			sch, raw := lbWithListeners(t, listeners)
			resp := &resource.ValidateConfigResponse{}
			(&lbResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
				Config: tfsdk.Config{Raw: raw, Schema: sch.Schema},
			}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("aucune erreur attendue, reçu %v", resp.Diagnostics)
			}
		})
	}
}

// La règle « exactement une cible » s'applique toujours aux backends connus.
func TestValidateConfig_KnownBackendStillValidated(t *testing.T) {
	sch, raw := lbWithListeners(t, func(lt tftypes.List) tftypes.Value {
		bt := backendType(lt)
		return tftypes.NewValue(lt, []tftypes.Value{
			listenerWithBackend(lt, tftypes.NewValue(bt, []tftypes.Value{
				nullsExcept(bt.ElementType.(tftypes.Object), map[string]tftypes.Value{
					"port": tftypes.NewValue(tftypes.Number, 8080),
				}),
			})),
		})
	})
	resp := &resource.ValidateConfigResponse{}
	(&lbResource{}).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
		Config: tfsdk.Config{Raw: raw, Schema: sch.Schema},
	}, resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("backend sans cible : erreur attendue")
	}
}

// En mise à jour, des backends inconnus au plan ne bloquent plus le plan et ne
// forcent pas le remplacement du LB (les backends se réconcilient en place).
func TestModifyPlan_UnknownBackendsNeitherFailNorReplace(t *testing.T) {
	bt := func(lt tftypes.List) tftypes.List { return backendType(lt) }
	sch, state := lbWithListeners(t, func(lt tftypes.List) tftypes.Value {
		return tftypes.NewValue(lt, []tftypes.Value{
			listenerWithBackend(lt, tftypes.NewValue(bt(lt), []tftypes.Value{})),
		})
	})
	_, plan := lbWithListeners(t, func(lt tftypes.List) tftypes.Value {
		return tftypes.NewValue(lt, []tftypes.Value{
			listenerWithBackend(lt, tftypes.NewValue(bt(lt), tftypes.UnknownValue)),
		})
	})
	resp := &resource.ModifyPlanResponse{Plan: tfsdk.Plan{Raw: plan, Schema: sch.Schema}}
	(&lbResource{}).ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		Plan:  tfsdk.Plan{Raw: plan, Schema: sch.Schema},
		State: tfsdk.State{Raw: state, Schema: sch.Schema},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("aucune erreur attendue, reçu %v", resp.Diagnostics)
	}
	if len(resp.RequiresReplace) != 0 {
		t.Fatalf("aucun remplacement attendu, reçu %v", resp.RequiresReplace)
	}
}
