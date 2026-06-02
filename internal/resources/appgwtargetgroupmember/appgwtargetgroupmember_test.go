// Tests unitaires ccp_appgw_target_group_member — régression sur le mapping
// de la réponse API (qui ne renvoie ni appgw_id ni target_group_id).
package appgwtargetgroupmember

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

func TestApplyToModel_PreservesTargetGroupIDWhenAPIOmitsIt(t *testing.T) {
	// La vraie API (AppgwTargetGroupMemberResponse) ne renvoie pas target_group_id.
	// applyToModel ne doit jamais écraser la valeur configurée par une chaîne vide —
	// sinon Terraform échoue avec "Provider produced inconsistent result after apply"
	// (fix v4.1.3).
	containerID := "00000000-0000-0000-0000-00000000c001"
	m := memberResourceModel{
		AppGWID:       types.StringValue("appgw-1"),
		TargetGroupID: types.StringValue("tg-from-config"),
	}
	mm := &client.AppGWTargetGroupMember{
		ID:          "member-1",
		ContainerID: &containerID,
		Port:        8000,
		Weight:      100,
		Enabled:     true,
		// TargetGroupID volontairement absent (zero value) — comme la vraie réponse API.
	}
	applyToModel(mm, &m)

	if got := m.TargetGroupID.ValueString(); got != "tg-from-config" {
		t.Fatalf("TargetGroupID doit être préservé quand l'API ne le renvoie pas, got %q", got)
	}
	if m.ID.ValueString() != "member-1" {
		t.Fatalf("ID doit être mappé depuis la réponse, got %q", m.ID.ValueString())
	}
	if m.Port.ValueInt64() != 8000 {
		t.Fatalf("Port doit être mappé depuis la réponse, got %d", m.Port.ValueInt64())
	}
}
