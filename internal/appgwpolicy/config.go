package appgwpolicy

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Configuration(ctx context.Context, config tfsdk.Config, name string, diags *diag.Diagnostics) types.Object {
	typ := L7Type()
	if name == "l4_policy" {
		typ = L4Type()
	}
	if name == "client_ip_policy" {
		typ = ClientIPType()
	}
	value := types.ObjectNull(typ.AttrTypes)
	if config.Raw.IsNull() {
		return value
	}
	diags.Append(config.GetAttribute(ctx, path.Root(name), &value)...)
	return value
}
