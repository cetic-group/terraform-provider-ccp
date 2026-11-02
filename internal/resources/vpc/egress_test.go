package vpc

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

// cetic-cloud-platform#1734 : l'IP de sortie internet est attribuée par la
// plateforme dès qu'un VNet du VPC a `snat = true`, et rendue quand plus aucun
// n'en a. Elle n'est donc pas une entrée de `ccp_vpc` : trois attributs en
// lecture seule, sans UseStateForUnknown (l'IP peut apparaître ou disparaître
// au gré des VNets), et aucune attente.

func vpcSchema(t *testing.T) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	(&vpcResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func TestEgressAttributesAreReadOnlyWithoutPlanModifier(t *testing.T) {
	s := vpcSchema(t)
	for _, name := range []string{"egress_ip", "egress_ip_type", "egress_ip_status"} {
		attr, ok := s.Attributes[name].(schema.StringAttribute)
		if !ok {
			t.Fatalf("%s: attribut absent ou de mauvais type", name)
		}
		if !attr.Computed || attr.Optional || attr.Required {
			t.Errorf("%s: doit être Computed seul (lecture seule)", name)
		}
		if len(attr.PlanModifiers) != 0 {
			t.Errorf("%s: aucun plan modifier attendu (UseStateForUnknown figerait une IP qui change au gré des VNets)", name)
		}
	}
}

func TestDedicatedEgressIPIsNotAnInput(t *testing.T) {
	if _, ok := vpcSchema(t).Attributes["dedicated_egress_ip"]; ok {
		t.Fatal("dedicated_egress_ip ne doit plus exister : la plateforme attribue l'IP d'office")
	}
}

func TestApplyVPCToModelEgress(t *testing.T) {
	ip, typ, st := "203.0.113.5", "edge", "attached"
	src := &client.VPC{ID: "v", Name: "n", Region: "RNN", Status: "active", CreatedAt: time.Now(),
		EgressIP: &ip, EgressIPType: &typ, EgressIPStatus: &st}
	var m vpcResourceModel
	if d := applyVPCToModel(context.Background(), src, &m); d.HasError() {
		t.Fatalf("apply: %v", d)
	}
	if m.EgressIP.ValueString() != ip || m.EgressIPType.ValueString() != typ ||
		m.EgressIPStatus.ValueString() != st {
		t.Fatalf("valeurs mal recopiées: %v %v %v", m.EgressIP, m.EgressIPType, m.EgressIPStatus)
	}
	// Plus aucun VNet `snat = true` : l'IP est rendue, les trois passent à null.
	src.EgressIP, src.EgressIPType, src.EgressIPStatus = nil, nil, nil
	if d := applyVPCToModel(context.Background(), src, &m); d.HasError() {
		t.Fatalf("apply: %v", d)
	}
	if !m.EgressIP.IsNull() || !m.EgressIPType.IsNull() || !m.EgressIPStatus.IsNull() {
		t.Fatal("sans IP de sortie, les trois attributs doivent être null")
	}
}
