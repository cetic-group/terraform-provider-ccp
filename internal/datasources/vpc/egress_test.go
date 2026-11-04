package vpc

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// cetic-cloud-platform#1734 : le contrat JSON de `VpcResponse`
// (dedicated_egress_ip, egress_ip, egress_ip_type, egress_ip_status, null sans
// IP de sortie dédiée) est décodé depuis
// une vraie réponse HTTP, puis projeté sur le modèle du data source. Un tag
// JSON mal orthographié ou un mapping oublié fait échouer ces tests.

func withEgress(base map[string]any, ip, typ, status any) map[string]any {
	base["egress_ip"], base["egress_ip_type"], base["egress_ip_status"] = ip, typ, status
	return base
}

func TestEgressDecodedFromGetAndList(t *testing.T) {
	avec := withEgress(fixture("vpc-1", "prod", "RNN"), "203.0.113.5", "edge", "attached")
	avec["dedicated_egress_ip"] = true
	sans := withEgress(fixture("vpc-2", "dev", "RNN"), nil, nil, nil)
	sans["dedicated_egress_ip"] = false
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/vpcs/vpc-1", Status: http.StatusOK, Body: avec},
		{Method: "GET", Path: "/v1/vpcs", Status: http.StatusOK, Body: []map[string]any{avec, sans}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")

	got, err := c.GetVPC(context.Background(), "vpc-1")
	if err != nil {
		t.Fatalf("GetVPC: %v", err)
	}
	list, err := c.ListVPCs(context.Background())
	if err != nil {
		t.Fatalf("ListVPCs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListVPCs: 2 VPC attendus, %d reçus", len(list))
	}
	for _, v := range []*client.VPC{got, &list[0]} {
		if v.EgressIP == nil || *v.EgressIP != "203.0.113.5" ||
			v.EgressIPType == nil || *v.EgressIPType != "edge" ||
			v.EgressIPStatus == nil || *v.EgressIPStatus != "attached" {
			t.Errorf("%s: egress mal décodé: %v %v %v", v.ID, v.EgressIP, v.EgressIPType, v.EgressIPStatus)
		}
	}
	if v := list[1]; v.EgressIP != nil || v.EgressIPType != nil || v.EgressIPStatus != nil {
		t.Errorf("vpc-2: null attendu sans IP de sortie, reçu %v %v %v", v.EgressIP, v.EgressIPType, v.EgressIPStatus)
	}

	if !got.DedicatedEgressIP || !list[0].DedicatedEgressIP || list[1].DedicatedEgressIP {
		t.Errorf("dedicated_egress_ip mal décodé: %v %v %v", got.DedicatedEgressIP,
			list[0].DedicatedEgressIP, list[1].DedicatedEgressIP)
	}

	m := stateFromVPC(context.Background(), got)
	if !m.DedicatedEgressIP.ValueBool() {
		t.Errorf("data source: dedicated_egress_ip = true attendu, reçu %v", m.DedicatedEgressIP)
	}
	if m.EgressIP.ValueString() != "203.0.113.5" || m.EgressIPType.ValueString() != "edge" ||
		m.EgressIPStatus.ValueString() != "attached" {
		t.Errorf("data source: egress mal recopié: %v %v %v", m.EgressIP, m.EgressIPType, m.EgressIPStatus)
	}
	m = stateFromVPC(context.Background(), &list[1])
	if m.DedicatedEgressIP.IsNull() || m.DedicatedEgressIP.ValueBool() {
		t.Errorf("data source: dedicated_egress_ip = false (non null) attendu, reçu %v", m.DedicatedEgressIP)
	}
	if !m.EgressIP.IsNull() || !m.EgressIPType.IsNull() || !m.EgressIPStatus.IsNull() {
		t.Errorf("data source: null attendu sans IP de sortie, reçu %v %v %v", m.EgressIP, m.EgressIPType, m.EgressIPStatus)
	}
	if m.ID.ValueString() != "vpc-2" || m.Cidr.ValueString() != "10.1.0.0/16" {
		t.Errorf("data source: champs de base mal recopiés: %v %v", m.ID, m.Cidr)
	}
}
