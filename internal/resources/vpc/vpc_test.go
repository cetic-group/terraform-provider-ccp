package vpc

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

// Régression : `terraform destroy` échouait avec
//
//	HTTP 409 — Le VPC contient des VNets — supprimez-les d'abord
//
// alors que les VNets enfants venaient d'être supprimés. Le Delete du VNet
// poll bien jusqu'au 404 dans la liste du VPC, mais le teardown backend
// (detach NIC NAT GW + IPAM + zone SDN) qui lève la précondition « has vnets »
// côté VPC se termine un peu après. Le DELETE du VPC doit donc réessayer sur
// 409 transitoire au lieu d'échouer dur. Cf. incident 2026-06-14.
func TestClassifyVPCDeleteAttempt(t *testing.T) {
	conflict := &client.APIError{
		StatusCode: http.StatusConflict,
		Method:     http.MethodDelete,
		Path:       "/v1/vpcs/abc",
		Detail:     "Le VPC contient des VNets — supprimez-les d'abord",
	}
	notFound := &client.APIError{StatusCode: http.StatusNotFound, Method: http.MethodDelete, Path: "/v1/vpcs/abc"}
	serverErr := &client.APIError{StatusCode: http.StatusInternalServerError, Method: http.MethodDelete, Path: "/v1/vpcs/abc"}
	transport := errors.New("connection refused")

	cases := []struct {
		name      string
		err       error
		wantDone  bool
		wantRetry bool
		wantFatal bool
	}{
		{"accepted", nil, true, false, false},              // 2xx → terminé
		{"already gone", notFound, true, false, false},     // 404 → déjà supprimé, succès
		{"contains vnets", conflict, false, true, false},   // 409 transitoire → réessayer
		{"server error", serverErr, false, false, true},    // 5xx → échec dur
		{"transport error", transport, false, false, true}, // non-API → échec dur
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done, retry, fatal := classifyVPCDeleteAttempt(c.err)
			if done != c.wantDone {
				t.Errorf("done = %v, want %v", done, c.wantDone)
			}
			if retry != c.wantRetry {
				t.Errorf("retry = %v, want %v", retry, c.wantRetry)
			}
			if (fatal != nil) != c.wantFatal {
				t.Errorf("fatal = %v, wantFatal = %v", fatal, c.wantFatal)
			}
			// Le 409 ne doit jamais remonter comme fatal : sinon le destroy
			// casse au lieu de réessayer (le bug d'origine).
			if c.err == conflict && fatal != nil {
				t.Error("le 409 « contient des VNets » ne doit pas être fatal")
			}
		})
	}
}
