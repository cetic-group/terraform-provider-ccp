package commit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// cetic-cloud-platform#2004 : l'API accepte aussi two_years et three_years.
func TestCommitTypeAccepteLesDureesDeLAPI(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	attr := resp.Schema.Attributes["commit_type"].(schema.StringAttribute)
	for _, v := range []string{"monthly", "yearly", "two_years", "three_years"} {
		var r validator.StringResponse
		for _, val := range attr.Validators {
			val.ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringValue(v)}, &r)
		}
		if r.Diagnostics.HasError() {
			t.Errorf("%s refusé : %v", v, r.Diagnostics)
		}
	}
}
