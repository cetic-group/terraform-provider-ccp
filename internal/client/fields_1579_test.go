package client

import (
	"encoding/json"
	"testing"
)

// #1579 (issue #87) — champs réseau et page d'indisponibilité relus depuis
// l'API, et `null` explicite au PATCH.
func TestDecode1579Fields(t *testing.T) {
	var lb LoadBalancer
	if err := json.Unmarshal([]byte(`{"id":"l","vpc_id":"v","vpc_name":"prod","vnet_name":"front","vnet_cidr":"10.0.1.0/24","custom_error_page_html":"<h1>x</h1>","listeners":[{"id":"x","tls_cert_not_after":"2027-01-01","backends":[{"id":"b","vm_scale_set_id":"ss","port":80,"weight":1}]}]}`), &lb); err != nil {
		t.Fatal(err)
	}
	if *lb.VpcName != "prod" || *lb.VnetCIDR != "10.0.1.0/24" || *lb.CustomErrorPageHTML != "<h1>x</h1>" ||
		*lb.Listeners[0].TLSCertNotAfter != "2027-01-01" || *lb.Listeners[0].Backends[0].VMScaleSetID != "ss" {
		t.Fatalf("champs #1579 mal décodés : %+v", lb)
	}
	var gw ApplicationGateway
	if err := json.Unmarshal([]byte(`{"id":"g","vpc_name":"prod","vnet_name":"front","vnet_cidr":"10.0.1.0/24"}`), &gw); err != nil {
		t.Fatal(err)
	}
	if *gw.VnetName != "front" {
		t.Fatalf("champs réseau AppGW mal décodés : %+v", gw)
	}
}

func TestNullableStringSendsExplicitNull(t *testing.T) {
	b, _ := json.Marshal(ApplicationGatewayUpdateRequest{CustomErrorPageHTML: &NullableString{}})
	if string(b) != `{"custom_error_page_html":null}` {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(ApplicationGatewayUpdateRequest{})
	if string(b) != `{}` {
		t.Fatalf("clé absente attendue quand rien ne change, got %s", b)
	}
}
