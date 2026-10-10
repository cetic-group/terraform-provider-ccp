// Package errorpage porte ce que `ccp_load_balancer` et
// `ccp_application_gateway` partagent pour `custom_error_page_html` (#1579) :
// la validation, le champ envoyé au PATCH et la relecture.
//
// Contrat amont : PATCH /v1/load-balancers/{id} et /v1/app-gateways/{id}
// écrivent le champ dès que sa CLÉ est présente ; `null` (ou une page vide)
// rétablit la page standard. Le serveur retire les espaces de début et de fin
// et refuse une page de plus de 7 Kio en octets UTF-8 ou contenant un NUL.
//
// L'attribut est donc `Optional` simple : la configuration fait autorité,
// absence comprise, parce que l'API sait revenir à « pas de page ». Retirer
// l'attribut du HCL doit alors envoyer `null` EXPLICITEMENT — d'où
// PatchValue, qui ne laisse jamais la clé absente quand la valeur change.
package errorpage

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// MaxBytes est le plafond amont (`MAX_CUSTOM_PAGE_BYTES = 7 * 1024`).
const MaxBytes = 7 * 1024

// Validator refuse au plan ce que l'API refuserait à l'apply : une page vide
// (l'API la lirait comme « page standard » et l'état ne convergerait pas), un
// caractère NUL, ou plus de 7 Kio une fois les espaces de bord retirés.
func Validator() validator.String { return pageValidator{} }

type pageValidator struct{}

func (pageValidator) Description(_ context.Context) string {
	return "non-empty HTML page, at most 7 KiB (UTF-8 bytes), without NUL characters"
}

func (v pageValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (pageValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	page := strings.TrimSpace(req.ConfigValue.ValueString())
	switch {
	case page == "":
		resp.Diagnostics.AddAttributeError(req.Path, "Empty error page",
			"The page is empty. Remove the attribute to restore the default page.")
	case strings.ContainsRune(page, 0):
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid error page",
			"The page contains a NUL character.")
	case len(page) > MaxBytes:
		resp.Diagnostics.AddAttributeError(req.Path, "Error page too large",
			fmt.Sprintf("The page is %d bytes; the limit is %d bytes. Inline the styles and host images elsewhere.",
				len(page), MaxBytes))
	}
}

// PatchValue rend le champ à mettre dans le PATCH, ou nil si rien ne change.
// Quand la page est retirée du HCL, il rend `null` explicite — jamais une clé
// absente, que l'API lirait comme « inchangé ».
func PatchValue(plan, state types.String) *client.NullableString {
	if plan.IsUnknown() || plan.Equal(state) {
		return nil
	}
	if plan.IsNull() {
		return &client.NullableString{}
	}
	v := plan.ValueString()
	return &client.NullableString{Value: &v}
}

// FromAPI projette la valeur relue sur l'état. Le serveur retire les espaces
// de bord : une page écrite en heredoc (terminée par un saut de ligne) revient
// sans lui. Si la valeur relue est exactement la valeur connue, une fois ses
// bords nettoyés, on garde la valeur connue — sinon le plan suivant
// proposerait de la réécrire, indéfiniment. Toute autre différence est une
// dérive réelle et remonte telle quelle.
func FromAPI(known types.String, api *string) types.String {
	if api == nil {
		return types.StringNull()
	}
	if !known.IsNull() && !known.IsUnknown() && strings.TrimSpace(known.ValueString()) == *api {
		return known
	}
	return types.StringValue(*api)
}
