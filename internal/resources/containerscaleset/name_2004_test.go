package containerscaleset

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// cetic-cloud-platform#2004 : l'API ne renomme pas un scale set.
func TestNameForceLeRemplacement(t *testing.T) {
	var resp resource.SchemaResponse
	New().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	for _, m := range resp.Schema.Attributes["name"].(schema.StringAttribute).PlanModifiers {
		if strings.Contains(m.Description(context.Background()), "recreate") {
			return
		}
	}
	t.Fatal("name doit forcer le remplacement")
}
