package vpcpeering

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// cetic-cloud-platform#2004 : l'API n'accepte aucun tag pour un peering ; des
// tags déclarés étaient ignorés et l'état devenait incohérent après apply.
func TestTagsEnLectureSeule(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr := resp.Schema.Attributes["tags"].(schema.ListAttribute)
	if attr.Optional || !attr.Computed {
		t.Fatalf("tags doit être Computed seulement (Optional=%v, Computed=%v)", attr.Optional, attr.Computed)
	}
	raw, _ := json.Marshal(client.VpcPeeringCreateRequest{Name: "p", VpcAID: "a", VpcBID: "b"})
	if string(raw) != `{"name":"p","vpc_a_id":"a","vpc_b_id":"b"}` {
		t.Fatalf("corps de création inattendu : %s", raw)
	}
}
