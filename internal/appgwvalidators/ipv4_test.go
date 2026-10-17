package appgwvalidators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestIPv4(t *testing.T) {
	cases := map[string]bool{
		"10.0.1.40":   true,
		"10.0.1.040":  false, // non canonique : l'API rendrait 10.0.1.40
		"10.0.1.400":  false,
		"2001:db8::1": false,
		"10.0.1.0/24": false,
		"":            false,
	}
	for in, ok := range cases {
		resp := &validator.StringResponse{}
		IPv4().ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("vip_address"), ConfigValue: types.StringValue(in),
		}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%q: attendu valide=%v", in, ok)
		}
	}
	for _, v := range []types.String{types.StringNull(), types.StringUnknown()} {
		resp := &validator.StringResponse{}
		IPv4().ValidateString(context.Background(), validator.StringRequest{ConfigValue: v}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("null/unknown must pass")
		}
	}
}
