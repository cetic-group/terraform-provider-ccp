// Tests for ccp_registry_user — focuses on the one-shot password
// preservation pattern (apikey-style).
package registryuser

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

func TestCreate_ReturnsPasswordOnce(t *testing.T) {
	body := map[string]any{
		"id":          "u-1",
		"registry_id": "reg-1",
		"username":    "ci-pull",
		"is_admin":    false,
		"password":    "p4ssword-once",
		"created_at":  "2026-05-09T10:00:00Z",
	}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "POST", Path: "/v1/registries/reg-1/users", Status: http.StatusCreated, Body: body},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.CreateRegistryUser(context.Background(), "reg-1", client.RegistryUserCreateRequest{
		Username: "ci-pull",
	})
	if err != nil {
		t.Fatalf("CreateRegistryUser: %v", err)
	}
	if got.Password != "p4ssword-once" {
		t.Fatalf("password not propagated, got %q", got.Password)
	}
	if got.IsAdmin {
		t.Fatalf("non-admin user reported is_admin=true")
	}
}

func TestList_ReadDoesNotReturnPassword(t *testing.T) {
	body := []map[string]any{{
		"id":          "u-1",
		"registry_id": "reg-1",
		"username":    "ci-pull",
		"is_admin":    false,
		"created_at":  "2026-05-09T10:00:00Z",
	}}
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/registries/reg-1/users", Status: http.StatusOK, Body: body},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	users, err := c.ListRegistryUsers(context.Background(), "reg-1")
	if err != nil {
		t.Fatalf("ListRegistryUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	// Decode raw to verify password is absent — no field for it on
	// RegistryUser by design.
	raw, _ := json.Marshal(users[0])
	if want := `"password"`; bytesContains(raw, want) {
		t.Errorf("Read shape unexpectedly contains password: %s", string(raw))
	}
}

func TestDelete_404IsSilent(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "DELETE", Path: "/v1/registries/reg-1/users/ci-pull", Status: http.StatusNotFound, Body: map[string]any{"detail": "not found"}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	err := c.DeleteRegistryUser(context.Background(), "reg-1", "ci-pull")
	if !client.IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestUsernameValidator(t *testing.T) {
	re := usernamePattern()
	for _, tc := range []struct {
		val   string
		valid bool
	}{
		{"ci-pull", true},
		{"ci123", true},
		{"a", true},
		{"CI-Pull", false}, // uppercase
		{"ci_pull", false}, // underscore not allowed
		{"ci pull", false}, // space
	} {
		got := re.MatchString(tc.val)
		if got != tc.valid {
			t.Errorf("username=%q valid=%v, got %v", tc.val, tc.valid, got)
		}
	}
}

func TestSplitImportID(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"reg/user", []string{"reg", "user"}},
		{"reg/", nil},
		{"/user", nil},
		{"reg-user", nil},
		{"", nil},
	} {
		got := splitImportID(tc.in)
		if (got == nil) != (tc.want == nil) {
			t.Errorf("splitImportID(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		if got == nil {
			continue
		}
		if got[0] != tc.want[0] || got[1] != tc.want[1] {
			t.Errorf("splitImportID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func bytesContains(haystack []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
