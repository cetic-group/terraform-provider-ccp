package appgwlistener

import (
	"encoding/json"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestListenerDomainCacheTriState(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    types.Bool
	}{
		{"historical null", `{"id":"listener-1","cache_enabled":null}`, types.BoolNull()},
		{"old API omitted", `{"id":"listener-1"}`, types.BoolNull()},
		{"explicitly disabled", `{"id":"listener-1","cache_enabled":false}`, types.BoolValue(false)},
		{"explicitly enabled", `{"id":"listener-1","cache_enabled":true}`, types.BoolValue(true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var l client.AppGWListener
			if err := json.Unmarshal([]byte(tc.payload), &l); err != nil {
				t.Fatal(err)
			}
			m := &listenerResourceModel{}
			applyToModel(&l, m)
			if !m.CacheEnabled.Equal(tc.want) {
				t.Fatalf("cache_enabled = %v, want %v", m.CacheEnabled, tc.want)
			}
		})
	}
}
