package appgwpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Only known values can become a write. Unknown is never an empty-object reset.
func native(value attr.Value) (any, error) {
	if value.IsUnknown() {
		return nil, fmt.Errorf("policy contains an unresolved value")
	}
	if value.IsNull() {
		return nil, nil
	}
	switch v := value.(type) {
	case types.String:
		return v.ValueString(), nil
	case types.Bool:
		return v.ValueBool(), nil
	case types.Object:
		result := map[string]any{}
		for key, child := range v.Attributes() {
			item, err := native(child)
			if err != nil {
				return nil, err
			}
			if item != nil {
				result[key] = item
			}
		}
		return result, nil
	case types.Map:
		result := map[string]any{}
		for key, child := range v.Elements() {
			item, err := native(child)
			if err != nil {
				return nil, err
			}
			if item == nil {
				return nil, fmt.Errorf("policy map entries cannot be null")
			}
			result[key] = item
		}
		return result, nil
	case types.List:
		result := []any{}
		for _, child := range v.Elements() {
			item, err := native(child)
			if err != nil {
				return nil, err
			}
			if item == nil {
				return nil, fmt.Errorf("policy list entries cannot be null")
			}
			result = append(result, item)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported policy attribute type")
	}
}
func keyedArray(value any) []any {
	entries, _ := value.(map[string]any)
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []any{}
	for _, key := range keys {
		item := entries[key].(map[string]any)
		item["key"] = key
		out = append(out, item)
	}
	return out
}
func l7Wire(root map[string]any) {
	if value, ok := root["policies"]; ok {
		policies := keyedArray(value)
		for _, entry := range policies {
			policy := entry.(map[string]any)
			for _, name := range []string{"allow_rules", "deny_rules", "required_headers"} {
				if collection, ok := policy[name]; ok {
					policy[name] = keyedArray(collection)
				}
			}
		}
		root["policies"] = policies
	}
}
func decode(value types.Object, kind string, out any) error {
	input, err := native(value)
	if err != nil {
		return err
	}
	if input == nil {
		return nil
	}
	root := input.(map[string]any)
	if kind == "l7" {
		l7Wire(root)
	}
	data, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("unable to encode access policy")
	}
	if len(data) > 65536 {
		return fmt.Errorf("policy exceeds 64 KiB")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("invalid access policy structure")
	}
	return nil
}
func L7(value types.Object) (*client.AppGWL7Policy, error) {
	if value.IsNull() {
		return nil, nil
	}
	out := new(client.AppGWL7Policy)
	err := decode(value, "l7", out)
	if err == nil {
		err = ValidateL7(out)
	}
	return out, err
}
func L4(value types.Object) (*client.AppGWL4Policy, error) {
	if value.IsNull() {
		return nil, nil
	}
	out := new(client.AppGWL4Policy)
	err := decode(value, "l4", out)
	if err == nil {
		err = ValidateL4(out)
	}
	return out, err
}
func ClientIP(value types.Object) (*client.AppGWClientIPPolicy, error) {
	if value.IsNull() {
		return nil, nil
	}
	out := new(client.AppGWClientIPPolicy)
	err := decode(value, "client_ip", out)
	if err == nil {
		err = ValidateClientIP(out)
	}
	return out, err
}

// Config determines ownership. The plan provides resolved values; imported or
// removed attributes must never accidentally become writes from computed state.
func Configured(config types.Object) bool           { return !config.IsNull() }
func Changed(config, plan, state types.Object) bool { return Configured(config) && !plan.Equal(state) }
func WriteError(diags *diag.Diagnostics, err error) bool {
	if err != nil {
		diags.AddError("Invalid Application Gateway access policy", err.Error())
		return true
	}
	return false
}

func arrayMap(value any) map[string]any {
	result := map[string]any{}
	entries, _ := value.([]any)
	for _, entry := range entries {
		item := entry.(map[string]any)
		key, _ := item["key"].(string)
		delete(item, "key")
		result[key] = item
	}
	return result
}
func l7HCL(root map[string]any) {
	if value, ok := root["policies"]; ok {
		policies := arrayMap(value)
		for _, entry := range policies {
			p := entry.(map[string]any)
			for _, name := range []string{"allow_rules", "deny_rules", "required_headers"} {
				if collection, ok := p[name]; ok {
					p[name] = arrayMap(collection)
				}
			}
		}
		root["policies"] = policies
	}
}
func attribute(typ attr.Type, value any) attr.Value {
	switch t := typ.(type) {
	case types.ObjectType:
		if value == nil {
			return types.ObjectNull(t.AttrTypes)
		}
		input := value.(map[string]any)
		attrs := map[string]attr.Value{}
		for key, child := range t.AttrTypes {
			attrs[key] = attribute(child, input[key])
		}
		return types.ObjectValueMust(t.AttrTypes, attrs)
	case types.MapType:
		if value == nil {
			return types.MapNull(t.ElemType)
		}
		input := value.(map[string]any)
		values := map[string]attr.Value{}
		for key, child := range input {
			values[key] = attribute(t.ElemType, child)
		}
		return types.MapValueMust(t.ElemType, values)
	case types.ListType:
		if value == nil {
			return types.ListNull(t.ElemType)
		}
		input := value.([]any)
		values := make([]attr.Value, 0, len(input))
		for _, child := range input {
			values = append(values, attribute(t.ElemType, child))
		}
		return types.ListValueMust(t.ElemType, values)
	case basetypes.StringType:
		return types.StringPointerValue(stringPointer(value))
	case basetypes.BoolType:
		if value == nil {
			return types.BoolNull()
		}
		return types.BoolValue(value.(bool))
	default:
		panic("unsupported access policy type")
	}
}
func stringPointer(value any) *string {
	if value == nil {
		return nil
	}
	v := value.(string)
	return &v
}
func objectFromAPI(api any, kind string, typ types.ObjectType) types.Object {
	data, _ := json.Marshal(api)
	var root map[string]any
	_ = json.Unmarshal(data, &root)
	if root == nil {
		return types.ObjectNull(typ.AttrTypes)
	}
	if kind == "l7" {
		l7HCL(root)
	}
	return attribute(typ, root).(types.Object)
}
func canonicalNetworks(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		canonical := value
		if prefix, err := netip.ParsePrefix(value); err == nil {
			canonical = prefix.Masked().String()
		} else if address, err := netip.ParseAddr(value); err == nil {
			canonical = netip.PrefixFrom(address, address.BitLen()).String()
		}
		if !seen[canonical] {
			out = append(out, canonical)
			seen[canonical] = true
		}
	}
	return out
}
func canonicalPath(path *client.AppGWSecurityPath) {
	if path == nil {
		return
	}
	if value, err := CanonicalPath(path.Value); err == nil {
		path.Value = value
	}
}
func normalize(policy any) []byte {
	if policy == nil || (reflect.ValueOf(policy).Kind() == reflect.Pointer && reflect.ValueOf(policy).IsNil()) {
		return []byte("null")
	}
	data, _ := json.Marshal(policy)
	switch policy.(type) {
	case *client.AppGWL7Policy:
		var p client.AppGWL7Policy
		_ = json.Unmarshal(data, &p)
		sort.Slice(p.Policies, func(i, j int) bool { return p.Policies[i].Key < p.Policies[j].Key })
		for i := range p.Policies {
			v := &p.Policies[i]
			if v.Scope != nil {
				canonicalPath(v.Scope.Path)
				if v.Scope.Path == nil {
					v.Scope = nil
				}
			}
			for _, rules := range [][]client.AppGWSecurityRule{v.AllowRules, v.DenyRules} {
				sort.Slice(rules, func(i, j int) bool { return rules[i].Key < rules[j].Key })
				for j := range rules {
					m := &rules[j].Match
					m.SourceCIDRs = canonicalNetworks(m.SourceCIDRs)
					m.Methods = unique(m.Methods)
					canonicalPath(m.Path)
					for k := range m.Headers {
						m.Headers[k].Name = strings.ToLower(m.Headers[k].Name)
						m.Headers[k].Values = unique(m.Headers[k].Values)
					}
				}
			}
			sort.Slice(v.RequiredHeaders, func(i, j int) bool { return v.RequiredHeaders[i].Key < v.RequiredHeaders[j].Key })
			for j := range v.RequiredHeaders {
				v.RequiredHeaders[j].Name = strings.ToLower(v.RequiredHeaders[j].Name)
				v.RequiredHeaders[j].Values = unique(v.RequiredHeaders[j].Values)
			}
		}
		data, _ = json.Marshal(p)
	case *client.AppGWL4Policy:
		var p client.AppGWL4Policy
		_ = json.Unmarshal(data, &p)
		for _, tcp := range []*client.AppGWTCPPolicy{p.TCP80, p.TCP443} {
			if tcp != nil {
				tcp.AllowCIDRs = canonicalNetworks(tcp.AllowCIDRs)
				tcp.DenyCIDRs = canonicalNetworks(tcp.DenyCIDRs)
			}
		}
		if p.TCP80 != nil && len(p.TCP80.AllowCIDRs)+len(p.TCP80.DenyCIDRs) == 0 {
			p.TCP80 = nil
		}
		if p.TCP443 != nil && len(p.TCP443.AllowCIDRs)+len(p.TCP443.DenyCIDRs) == 0 {
			p.TCP443 = nil
		}
		data, _ = json.Marshal(p)
	case *client.AppGWClientIPPolicy:
		var p client.AppGWClientIPPolicy
		_ = json.Unmarshal(data, &p)
		if p.Mode == "peer" {
			p.Mode = ""
		}
		p.TrustedProxyCIDRs = canonicalNetworks(p.TrustedProxyCIDRs)
		data, _ = json.Marshal(p)
	}
	return data
}
func FromL7(reference types.Object, api *client.AppGWL7Policy) types.Object {
	if api != nil && !reference.IsNull() && !reference.IsUnknown() {
		if old, err := L7(reference); err == nil && string(normalize(old)) == string(normalize(api)) {
			return reference
		}
	}
	return objectFromAPI(api, "l7", L7Type())
}
func FromL4(reference types.Object, api *client.AppGWL4Policy) types.Object {
	if api != nil && !reference.IsNull() && !reference.IsUnknown() {
		if old, err := L4(reference); err == nil && string(normalize(old)) == string(normalize(api)) {
			return reference
		}
	}
	return objectFromAPI(api, "l4", L4Type())
}
func FromClientIP(reference types.Object, api *client.AppGWClientIPPolicy) types.Object {
	if api != nil && !reference.IsNull() && !reference.IsUnknown() {
		if old, err := ClientIP(reference); err == nil && string(normalize(old)) == string(normalize(api)) {
			return reference
		}
	}
	return objectFromAPI(api, "client_ip", ClientIPType())
}

func Confirm(expected, actual any, diags *diag.Diagnostics) bool {
	if string(normalize(expected)) != string(normalize(actual)) {
		diags.AddError("Access policy not confirmed", "The platform returned a different policy. Refresh the resource before reapplying; protection is not confirmed.")
		return false
	}
	return true
}
func Wait(ctx context.Context, c *client.Client, id string, revision *int64, diags *diag.Diagnostics) *client.ApplicationGateway {
	if revision == nil {
		diags.AddError("Access policy not confirmed", "The platform did not return its policy revision. Refresh the resource before retrying.")
		return nil
	}
	gateway, err := c.WaitAppGWAccessPolicy(ctx, id, *revision, 10*time.Minute)
	if err != nil {
		diags.AddError("Access policy application not confirmed", err.Error())
		return nil
	}
	return gateway
}

func GatewayConfirmed(expectedL4 *client.AppGWL4Policy, expectedL7 *client.AppGWL7Policy, expectedIP *client.AppGWClientIPPolicy, gateway *client.ApplicationGateway, diags *diag.Diagnostics) bool {
	if gateway == nil {
		return false
	}
	ok := true
	if expectedL4 != nil {
		ok = Confirm(expectedL4, gateway.L4Policy, diags) && ok
	}
	if expectedL7 != nil {
		ok = Confirm(expectedL7, gateway.L7Policy, diags) && ok
	}
	if expectedIP != nil {
		ok = Confirm(expectedIP, gateway.ClientIPPolicy, diags) && ok
	}
	return ok
}

func unique(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}
