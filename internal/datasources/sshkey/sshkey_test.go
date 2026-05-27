package sshkey

import (
	"context"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client/testutil"
)

func TestLookupByID(t *testing.T) {
	srv := testutil.NewTestServer(t, testutil.Routes{
		{Method: "GET", Path: "/v1/ssh-keys", Status: http.StatusOK, Body: []map[string]any{
			{"id": "k-1", "name": "laptop", "fingerprint": "SHA256:abc", "scope": "user", "created_at": "2026-05-25T10:00:00Z"},
		}},
	})
	defer srv.Close()
	c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
	got, err := c.GetSSHKey(context.Background(), "k-1")
	if err != nil {
		t.Fatalf("GetSSHKey: %v", err)
	}
	if got.Fingerprint != "SHA256:abc" {
		t.Errorf("unexpected: %+v", got)
	}
}
