package errorpage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func patchJSON(t *testing.T, plan, state types.String) string {
	t.Helper()
	body := client.LoadBalancerUpdateRequest{CustomErrorPageHTML: PatchValue(plan, state)}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Le cœur du ticket : retirer la page du HCL doit ENVOYER `null`. Une clé
// absente serait lue « inchangée » et le diff ne convergerait jamais.
func TestRemovingThePageSendsExplicitNull(t *testing.T) {
	got := patchJSON(t, types.StringNull(), types.StringValue("<h1>maintenance</h1>"))
	if got != `{"custom_error_page_html":null}` {
		t.Fatalf("retrait : attendu une clé à null, got %s", got)
	}
}

func TestSettingThePageSendsIt(t *testing.T) {
	got := patchJSON(t, types.StringValue("<h1>x</h1>"), types.StringNull())
	var m map[string]*string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["custom_error_page_html"]; !ok || v == nil || *v != "<h1>x</h1>" {
		t.Fatalf("got %s", got)
	}
}

func TestUnchangedPageSendsNothing(t *testing.T) {
	if got := patchJSON(t, types.StringValue("a"), types.StringValue("a")); got != `{}` {
		t.Fatalf("inchangée : aucune clé attendue, got %s", got)
	}
	if got := patchJSON(t, types.StringNull(), types.StringNull()); got != `{}` {
		t.Fatalf("absente des deux côtés : aucune clé attendue, got %s", got)
	}
}

// Une page en heredoc finit par un saut de ligne que le serveur retire : la
// relecture doit garder la valeur connue, sinon perma-diff.
func TestFromAPIKeepsKnownValueWhenOnlyEdgesDiffer(t *testing.T) {
	known := types.StringValue("<h1>maintenance</h1>\n")
	api := "<h1>maintenance</h1>"
	if got := FromAPI(known, &api); !got.Equal(known) {
		t.Fatalf("bords seuls : valeur connue attendue, got %v", got)
	}
	other := "<h1>autre</h1>"
	if got := FromAPI(known, &other); got.ValueString() != other {
		t.Fatalf("dérive réelle : valeur relue attendue, got %v", got)
	}
	if got := FromAPI(known, nil); !got.IsNull() {
		t.Fatalf("page standard côté serveur : null attendu, got %v", got)
	}
}

func TestValidator(t *testing.T) {
	cases := map[string]bool{
		"<h1>ok</h1>":                     true,
		"   \n ":                          false,
		"a\x00b":                          false,
		strings.Repeat("é", MaxBytes/2):   true, // 2 octets par « é » : pile 7 Kio
		strings.Repeat("é", MaxBytes/2+1): false,
	}
	for in, ok := range cases {
		var resp validator.StringResponse
		Validator().ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("custom_error_page_html"), ConfigValue: types.StringValue(in),
		}, &resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("len=%d : valide attendu=%v, diags=%v", len(in), ok, resp.Diagnostics)
		}
	}
}
