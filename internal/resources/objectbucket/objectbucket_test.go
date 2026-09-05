// Unit tests for ccp_object_bucket — credentials handling when the API
// refuses (#72). Acceptance tests (TF_ACC=1) live in the consuming modules
// repo.
package objectbucket

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/client/testutil"
)

// errFromCredentials drives the REAL client against a test server so the
// error under test is the one production builds — not a hand-made value that
// happens to satisfy the helper.
func errFromCredentials(t *testing.T, status int, detail string) error {
	t.Helper()
	id := "b-1"
	srv := testutil.NewTestServer(t, testutil.Routes{
		{
			Method: "GET", Path: "/v1/buckets/" + id + "/credentials",
			Status: status, Body: map[string]any{"detail": detail},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	_, err := c.GetObjectBucketCredentials(context.Background(), id)
	if err == nil {
		t.Fatalf("expected an error for HTTP %d", status)
	}
	return err
}

// ⚠️ THE test of #72. Both attributes are Computed, so before apply Terraform
// holds them as unknown. Leaving them untouched on failure kept them unknown,
// and the protocol forbids that after apply:
//
//	Error: Provider returned invalid result object after apply
//	  […] still indicated an unknown value for […].access_key.
//
// Null is not a fallback — it states the truth: this key cannot read them.
func TestBlankCredentials_LeavesNullNotUnknown(t *testing.T) {
	m := &objectBucketResourceModel{
		AccessKey: types.StringUnknown(),
		SecretKey: types.StringUnknown(),
	}

	blankCredentials(m)

	for name, v := range map[string]types.String{
		"access_key": m.AccessKey, "secret_key": m.SecretKey,
	} {
		if v.IsUnknown() {
			t.Fatalf("%s is still unknown after apply — Terraform rejects the "+
				"whole apply as a provider bug", name)
		}
		if !v.IsNull() {
			t.Fatalf("%s should be null, got %q", name, v.ValueString())
		}
	}
}

// A 403 is an IAM decision, never a latency. Telling the user to "re-run once
// provisioning settles" sends them chasing something that will never happen:
// an explicit Deny beats any Allow, and attaching another role changes
// nothing.
func TestCredentialsDetail_ForbiddenIsPermanent(t *testing.T) {
	err := errFromCredentials(t, http.StatusForbidden, "Accès refusé.")

	if !client.IsForbidden(err) {
		t.Fatalf("the client no longer reports 403 as forbidden: %v", err)
	}
	detail := credentialsUnavailableDetail("b-1", err)

	if strings.Contains(detail, "provisioning settles") {
		t.Fatalf("a permanent IAM refusal is described as a latency:\n%s", detail)
	}
	for _, attendu := range []string{"permanent", "null", "admin-scoped"} {
		if !strings.Contains(detail, attendu) {
			t.Fatalf("the message does not say %q — the user cannot act on it:\n%s",
				attendu, detail)
		}
	}
}

// The witness: without it, "never mention provisioning" would pass by simply
// deleting the transient wording, and a real 409 would stop telling the user
// to retry.
func TestCredentialsDetail_ConflictStillSuggestsRetry(t *testing.T) {
	err := errFromCredentials(t, http.StatusConflict, "Bucket en cours de création.")

	if client.IsForbidden(err) {
		t.Fatalf("409 must not be treated as a refusal")
	}
	detail := credentialsUnavailableDetail("b-1", err)
	if !strings.Contains(detail, "provisioning settles") {
		t.Fatalf("a transient failure no longer suggests retrying:\n%s", detail)
	}
	if strings.Contains(detail, "permanent") {
		t.Fatalf("a transient failure is described as permanent:\n%s", detail)
	}
}

// Un couple de credentials nul signale un refus permanent : rappeler
// `/credentials` a chaque Read ne peut rien changer, et imprime un
// avertissement que le practitioner ne peut pas lever. On saute donc l'appel.
func TestCredentialsWorthRefreshing(t *testing.T) {
	cas := []struct {
		nom       string
		accessKey types.String
		secretKey types.String
		attendu   bool
	}{
		{
			nom:       "les deux nuls — refus permanent, on n'appelle pas",
			accessKey: types.StringNull(),
			secretKey: types.StringNull(),
			attendu:   false,
		},
		{
			nom:       "credentials en etat — on rafraichit pour capter une rotation",
			accessKey: types.StringValue("AKIAEXAMPLE"),
			secretKey: types.StringValue("s3cr3t"),
			attendu:   true,
		},
		{
			nom:       "moitie renseignee — etat incoherent, on rafraichit",
			accessKey: types.StringValue("AKIAEXAMPLE"),
			secretKey: types.StringNull(),
			attendu:   true,
		},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			m := &objectBucketResourceModel{AccessKey: c.accessKey, SecretKey: c.secretKey}
			if got := credentialsWorthRefreshing(m); got != c.attendu {
				t.Fatalf("credentialsWorthRefreshing = %v, attendu %v", got, c.attendu)
			}
		})
	}
}
