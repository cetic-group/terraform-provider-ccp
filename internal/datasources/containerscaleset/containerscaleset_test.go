package containerscaleset

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/container-scale-sets/ss-1", Status: http.StatusOK, Body: map[string]any{
			"id": "ss-1", "name": "edges", "region": "RNN", "plan": "small", "template": "alpine-3.20",
			"min_instances": 1, "max_instances": 5, "desired_instances": 2, "auto_repair": false,
			"status": "active", "tags": []string{},
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetContainerScaleSet(context.Background(), "ss-1")
	if err != nil {
		t.Fatalf("GetContainerScaleSet: %v", err)
	}
	if got.MinInstances != 1 || got.MaxInstances != 5 {
		t.Errorf("unexpected bounds: %+v", got)
	}
}

// ⚠️ LE test de #75 : le détail PORTE déjà les membres, le provider les jetait.
// Sans eux, aucun chemin déclaratif ne relie un scale set à une Application
// Gateway — `ccp_appgw_target_group_member` n'accepte qu'un `container_id`.
func TestDetail_PorteLesMembres(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/container-scale-sets/ss-1", Status: http.StatusOK, Body: map[string]any{
			"id": "ss-1", "name": "edges", "region": "RNN", "plan": "small", "template": "alpine-3.20",
			"min_instances": 1, "max_instances": 5, "desired_instances": 2, "auto_repair": false,
			"status": "active", "tags": []string{},
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
			"containers": []map[string]any{
				{"id": "c-1", "name": "edges-1", "status": "running", "ip_address": "10.0.1.11"},
				// Un membre encore en création n'a pas d'IP : le champ doit
				// rester null, pas devenir la chaîne vide — un backend pointé
				// sur "" serait accepté puis injoignable.
				{"id": "c-2", "name": "edges-2", "status": "creating"},
			},
		}},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetContainerScaleSet(context.Background(), "ss-1")
	if err != nil {
		t.Fatalf("GetContainerScaleSet: %v", err)
	}

	if len(got.Containers) != 2 {
		t.Fatalf("le détail ne porte pas ses membres : %d", len(got.Containers))
	}
	if got.Containers[0].IPAddress == nil || *got.Containers[0].IPAddress != "10.0.1.11" {
		t.Fatalf("IP du premier membre perdue : %+v", got.Containers[0])
	}
	if got.Containers[1].IPAddress != nil {
		t.Fatalf("un membre sans IP doit rester null, pas %q", *got.Containers[1].IPAddress)
	}
}

// Le témoin : la LISTE ne porte pas les membres. C'est ce qui oblige la data
// source à relire le détail quand la recherche passe par `(name, region)` —
// sans quoi l'attribut serait vide ou plein selon la forme de la recherche.
func TestListe_NePortePasLesMembres(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/container-scale-sets", Status: http.StatusOK, Body: []map[string]any{{
			"id": "ss-1", "name": "edges", "region": "RNN", "plan": "small", "template": "alpine-3.20",
			"min_instances": 1, "max_instances": 5, "desired_instances": 2, "auto_repair": false,
			"status": "active", "tags": []string{},
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
		}}},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	list, err := c.ListContainerScaleSets(context.Background(), "RNN")
	if err != nil {
		t.Fatalf("ListContainerScaleSets: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("attendu 1 scale set, %d", len(list))
	}
	if list[0].Containers != nil {
		t.Fatalf("la liste porte désormais les membres : la relecture du "+
			"détail devient inutile, revoir la data source (%d membres)",
			len(list[0].Containers))
	}
}

// ⚠️ LE test de #75 : il pilote le `Read` de la data source, pas le client.
// Une couverture qui s'arrête au client laisse passer la vraie régression —
// vérifié par mutation : retirer la projection dans `Read` ne faisait tomber
// aucun test tant que celui-ci n'existait pas.
func TestRead_ProjetteLesMembresDansLEtat(t *testing.T) {
	ctx := context.Background()
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/container-scale-sets/ss-1", Status: http.StatusOK, Body: map[string]any{
			"id": "ss-1", "name": "edges", "region": "RNN", "plan": "small", "template": "alpine-3.20",
			"min_instances": 1, "max_instances": 5, "desired_instances": 2, "auto_repair": false,
			"status": "active", "tags": []string{},
			"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
			"containers": []map[string]any{
				{"id": "m-1", "name": "edges-1", "status": "running", "ip_address": "10.0.1.11"},
				{"id": "m-2", "name": "edges-2", "status": "creating"},
			},
		}},
	})
	defer srv.Close()

	d := &cssDS{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sresp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sresp)

	objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	champs := map[string]tftypes.Value{}
	for nom, typ := range objType.AttributeTypes {
		champs[nom] = tftypes.NewValue(typ, nil)
	}
	champs["id"] = tftypes.NewValue(tftypes.String, "ss-1")

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sresp.Schema}}
	d.Read(ctx, datasource.ReadRequest{
		Config: tfsdk.Config{Raw: tftypes.NewValue(objType, champs), Schema: sresp.Schema},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics.Errors())
	}

	var etat cssDSModel
	if diags := resp.State.Get(ctx, &etat); diags.HasError() {
		t.Fatalf("State.Get: %v", diags.Errors())
	}
	if etat.Containers.IsNull() || len(etat.Containers.Elements()) != 2 {
		t.Fatalf("les membres n'atteignent pas l'état : %#v", etat.Containers)
	}

	var membres []membreModel
	if diags := etat.Containers.ElementsAs(ctx, &membres, false); diags.HasError() {
		t.Fatalf("ElementsAs: %v", diags.Errors())
	}
	if membres[0].ID.ValueString() != "m-1" || membres[0].IPAddress.ValueString() != "10.0.1.11" {
		t.Fatalf("premier membre mal projeté : %+v", membres[0])
	}
	// Un membre sans adresse doit rester null : un backend pointé sur la
	// chaîne vide serait accepté puis injoignable.
	if !membres[1].IPAddress.IsNull() {
		t.Fatalf("un membre sans IP doit rester null, pas %q", membres[1].IPAddress.ValueString())
	}
}

// ⚠️ La recherche par `(name, region)` passe par la LISTE, qui ne porte pas les
// membres : sans relecture du détail, l'attribut serait vide ou plein selon la
// FORME de la recherche, pour le même scale set. Une incohérence silencieuse,
// et impossible à diagnostiquer depuis une configuration.
func TestRead_ParNom_RelitLeDetailPourLesMembres(t *testing.T) {
	ctx := context.Background()
	base := map[string]any{
		"id": "ss-1", "name": "edges", "region": "RNN", "plan": "small", "template": "alpine-3.20",
		"min_instances": 1, "max_instances": 5, "desired_instances": 2, "auto_repair": false,
		"status": "active", "tags": []string{},
		"created_at": "2026-05-25T10:00:00Z", "updated_at": "2026-05-25T10:05:00Z",
	}
	detail := map[string]any{}
	for k, v := range base {
		detail[k] = v
	}
	detail["containers"] = []map[string]any{
		{"id": "m-1", "name": "edges-1", "status": "running", "ip_address": "10.0.1.11"},
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/container-scale-sets", Status: http.StatusOK, Body: []map[string]any{base}},
		{Method: "GET", Path: "/v1/container-scale-sets/ss-1", Status: http.StatusOK, Body: detail},
	})
	defer srv.Close()

	d := &cssDS{client: client.New(srv.URL, "ccp_test_unit", "0.0.0-test")}
	var sresp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &sresp)

	objType := sresp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	champs := map[string]tftypes.Value{}
	for nom, typ := range objType.AttributeTypes {
		champs[nom] = tftypes.NewValue(typ, nil)
	}
	champs["name"] = tftypes.NewValue(tftypes.String, "edges")
	champs["region"] = tftypes.NewValue(tftypes.String, "RNN")

	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: sresp.Schema}}
	d.Read(ctx, datasource.ReadRequest{
		Config: tfsdk.Config{Raw: tftypes.NewValue(objType, champs), Schema: sresp.Schema},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics.Errors())
	}

	var etat cssDSModel
	if diags := resp.State.Get(ctx, &etat); diags.HasError() {
		t.Fatalf("State.Get: %v", diags.Errors())
	}
	if etat.Containers.IsNull() || len(etat.Containers.Elements()) != 1 {
		t.Fatalf("recherche par nom : les membres manquent alors que le détail "+
			"les porte — %#v", etat.Containers)
	}
}
