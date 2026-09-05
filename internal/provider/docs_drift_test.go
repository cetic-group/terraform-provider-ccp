// La documentation publiée doit décrire le schéma LIVRÉ (#75).
package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rsschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// ⚠️ **Pourquoi ce test existe.** La page de `ccp_container_scale_set`
// documentait `replicas`, `min_replicas`, `max_replicas` et `current_replicas`
// alors que le provider livre `desired_instances`, `min_instances`,
// `max_instances` — et rien pour le quatrième. L'exemple de la page échouait
// donc tel quel sur `Unsupported argument`. Même chose sur
// `ccp_vnet_firewall_rule`, qui décrivait `src_cidr` / `dst_cidr` là où la v6
// attend `source_cidr` / `dest_cidr`.
//
// Une relecture ne l'attrape pas : les deux fichiers sont justes séparément,
// c'est leur ACCORD qui manque. On le vérifie donc contre les schémas réels,
// obtenus en instanciant chaque ressource — pas en relisant le source.
//
// ⚠️ `_DETTE` liste les pages encore en écart, constatées en posant ce test.
// Elles ne sont pas corrigées ici parce que le bon libellé demande de savoir ce
// que la page VOULAIT dire — le deviner rendrait la doc fausse dans l'autre
// sens. La liste est là pour être vidée, pas pour durer.
var _DETTE = map[string][]string{
	"api_key":             {"access_key_prefix", "expires_at", "last_used_at"},
	"iam_role_assignment": {"api_key", "ccks_workload", "org_member", "service_account"},
	"k8s_cluster":         {"endpoint", "kubeconfig"},
	"org_member":          {"status"},
	"quota_request":       {"granted_value"},
	"registry":            {"last_activity_at", "public_ip", "storage_used_mb"},
	"support_ticket":      {"ticket_number"},
}

var _reAttrDoc = regexp.MustCompile("(?m)^-\\s+`([a-z_][a-z0-9_]*)`")

// attributsDocumentes lit les puces `- \x60nom\x60` d'une page.
func attributsDocumentes(t *testing.T, chemin string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(chemin)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, m := range _reAttrDoc.FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	return out
}

// attributsDuSchema descend récursivement : un attribut imbriqué est documenté
// comme les autres, et l'ignorer produirait de faux écarts.
func attributsDuSchema(attrs map[string]rsschema.Attribute, blocs map[string]rsschema.Block) map[string]bool {
	out := map[string]bool{}
	var visiteA func(map[string]rsschema.Attribute)
	visiteA = func(m map[string]rsschema.Attribute) {
		for nom, a := range m {
			out[nom] = true
			switch v := a.(type) {
			case rsschema.SingleNestedAttribute:
				visiteA(v.Attributes)
			case rsschema.ListNestedAttribute:
				visiteA(v.NestedObject.Attributes)
			case rsschema.SetNestedAttribute:
				visiteA(v.NestedObject.Attributes)
			case rsschema.MapNestedAttribute:
				visiteA(v.NestedObject.Attributes)
			}
		}
	}
	visiteA(attrs)
	var visiteB func(map[string]rsschema.Block)
	visiteB = func(m map[string]rsschema.Block) {
		for nom, b := range m {
			out[nom] = true
			switch v := b.(type) {
			case rsschema.ListNestedBlock:
				visiteA(v.NestedObject.Attributes)
				visiteB(v.NestedObject.Blocks)
			case rsschema.SetNestedBlock:
				visiteA(v.NestedObject.Attributes)
				visiteB(v.NestedObject.Blocks)
			case rsschema.SingleNestedBlock:
				visiteA(v.Attributes)
				visiteB(v.Blocks)
			}
		}
	}
	visiteB(blocs)
	return out
}

// ⚠️ Les BLOCS comptent aussi. Une première version ne parcourait que les
// attributs et accusait `iam_policy_document` de documenter un `statement`
// inexistant — alors que c'est un `ListNestedBlock`. Le test aurait fait
// supprimer une ligne de documentation juste.
func attributsDuSchemaDS(attrs map[string]dsschema.Attribute, blocs map[string]dsschema.Block) map[string]bool {
	out := map[string]bool{}
	var visite func(map[string]dsschema.Attribute)
	visite = func(m map[string]dsschema.Attribute) {
		for nom, a := range m {
			out[nom] = true
			switch v := a.(type) {
			case dsschema.SingleNestedAttribute:
				visite(v.Attributes)
			case dsschema.ListNestedAttribute:
				visite(v.NestedObject.Attributes)
			case dsschema.SetNestedAttribute:
				visite(v.NestedObject.Attributes)
			case dsschema.MapNestedAttribute:
				visite(v.NestedObject.Attributes)
			}
		}
	}
	visite(attrs)

	var visiteB func(map[string]dsschema.Block)
	visiteB = func(m map[string]dsschema.Block) {
		for nom, b := range m {
			out[nom] = true
			switch v := b.(type) {
			case dsschema.ListNestedBlock:
				visite(v.NestedObject.Attributes)
				visiteB(v.NestedObject.Blocks)
			case dsschema.SetNestedBlock:
				visite(v.NestedObject.Attributes)
				visiteB(v.NestedObject.Blocks)
			case dsschema.SingleNestedBlock:
				visite(v.Attributes)
				visiteB(v.Blocks)
			}
		}
	}
	visiteB(blocs)
	return out
}

func TestDoc_NeDecritQueDesAttributsLIVRES(t *testing.T) {
	ctx := context.Background()
	p := &ccpProvider{}
	var fautes []string

	for _, mk := range p.Resources(ctx) {
		r := mk()
		var mres resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "ccp"}, &mres)
		var sres resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sres)

		court := strings.TrimPrefix(mres.TypeName, "ccp_")
		doc := attributsDocumentes(t, filepath.Join("..", "..", "docs", "resources", court+".md"))
		if doc == nil {
			continue // pas de page : ce n'est pas l'objet de ce test
		}
		reel := attributsDuSchema(sres.Schema.Attributes, sres.Schema.Blocks)
		toleres := map[string]bool{}
		for _, a := range _DETTE[court] {
			toleres[a] = true
		}
		var manquants []string
		for nom := range doc {
			if !reel[nom] && !toleres[nom] {
				manquants = append(manquants, nom)
			}
		}
		sort.Strings(manquants)
		if len(manquants) > 0 {
			fautes = append(fautes, "  docs/resources/"+court+".md décrit "+
				strings.Join(manquants, ", ")+" — absent(s) du schéma livré")
		}
	}

	for _, mk := range p.DataSources(ctx) {
		d := mk()
		var mres datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "ccp"}, &mres)
		var sres datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &sres)

		court := strings.TrimPrefix(mres.TypeName, "ccp_")
		doc := attributsDocumentes(t, filepath.Join("..", "..", "docs", "data-sources", court+".md"))
		if doc == nil {
			continue
		}
		reel := attributsDuSchemaDS(sres.Schema.Attributes, sres.Schema.Blocks)
		var manquants []string
		for nom := range doc {
			if !reel[nom] {
				manquants = append(manquants, nom)
			}
		}
		sort.Strings(manquants)
		if len(manquants) > 0 {
			fautes = append(fautes, "  docs/data-sources/"+court+".md décrit "+
				strings.Join(manquants, ", ")+" — absent(s) du schéma livré")
		}
	}

	if len(fautes) > 0 {
		sort.Strings(fautes)
		t.Fatalf("la documentation publiée décrit des attributs que le provider "+
			"ne livre pas — un `terraform apply` copié depuis ces pages echoue "+
			"sur `Unsupported argument` :\n%s", strings.Join(fautes, "\n"))
	}
}
