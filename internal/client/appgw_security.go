package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type AppGWSecurityPath struct {
	Op    string `json:"op"`
	Value string `json:"value"`
}
type AppGWSecurityScope struct {
	Path *AppGWSecurityPath `json:"path,omitempty"`
}
type AppGWSecurityHeader struct {
	Key    string   `json:"key,omitempty"`
	Name   string   `json:"name"`
	Op     string   `json:"op"`
	Values []string `json:"values,omitempty"`
}
type AppGWSecurityMatch struct {
	SourceCIDRs []string              `json:"source_cidrs,omitempty"`
	Path        *AppGWSecurityPath    `json:"path,omitempty"`
	Methods     []string              `json:"methods,omitempty"`
	Headers     []AppGWSecurityHeader `json:"headers,omitempty"`
	MatchAll    *bool                 `json:"match_all,omitempty"`
}
type AppGWSecurityRule struct {
	Key   string             `json:"key"`
	Match AppGWSecurityMatch `json:"match"`
}
type AppGWHTTPPolicy struct {
	Key             string                `json:"key"`
	Scope           *AppGWSecurityScope   `json:"scope,omitempty"`
	AllowRules      []AppGWSecurityRule   `json:"allow_rules,omitempty"`
	DenyRules       []AppGWSecurityRule   `json:"deny_rules,omitempty"`
	RequiredHeaders []AppGWSecurityHeader `json:"required_headers,omitempty"`
}
type AppGWL7Policy struct {
	Enabled  bool              `json:"enabled,omitempty"`
	Policies []AppGWHTTPPolicy `json:"policies,omitempty"`
}
type AppGWTCPPolicy struct {
	AllowCIDRs []string `json:"allow_cidrs,omitempty"`
	DenyCIDRs  []string `json:"deny_cidrs,omitempty"`
}
type AppGWL4Policy struct {
	Enabled bool            `json:"enabled,omitempty"`
	TCP80   *AppGWTCPPolicy `json:"tcp_80,omitempty"`
	TCP443  *AppGWTCPPolicy `json:"tcp_443,omitempty"`
}
type AppGWClientIPPolicy struct {
	Mode              string   `json:"mode,omitempty"`
	TrustedProxyCIDRs []string `json:"trusted_proxy_cidrs,omitempty"`
}
type AppGWAccessReplica struct {
	State              string `json:"state"`
	Generation         int64  `json:"generation"`
	RevocationComplete bool   `json:"revocation_complete"`
}
type AppGWAccessStatus struct {
	Generation int64                         `json:"generation"`
	State      string                        `json:"state"`
	Replicas   map[string]AppGWAccessReplica `json:"replicas"`
}
type AppGWListenerUpdateRequest struct {
	L7Policy         *AppGWL7Policy `json:"l7_policy,omitempty"`
	SecurityRevision *int64         `json:"security_revision,omitempty"`
}

// Policy errors never echo submitted header values from a server diagnostic.
func AppGWPolicyError(err error) error {
	if err == nil {
		return nil
	}
	var api *APIError
	if errors.As(err, &api) {
		switch api.StatusCode {
		case http.StatusConflict:
			return errors.New("access policy changed concurrently; refresh the resource and reapply")
		case http.StatusForbidden:
			return errors.New("access policy update is not permitted")
		case http.StatusUnprocessableEntity:
			return errors.New("access policy rejected; check criteria, limits, certificate and cache compatibility")
		default:
			return fmt.Errorf("access policy request failed (HTTP %d)", api.StatusCode)
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("unable to confirm access policy request; refresh before retrying")
}
func (c *Client) RequireAppGWPolicies(ctx context.Context, nonEmpty ...bool) error {
	var caps struct {
		Versions []int `json:"appgw_security_policy_versions"`
		Enabled  bool  `json:"appgw_security_policies_enabled"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/platform/capabilities", nil, &caps); err != nil {
		return AppGWPolicyError(err)
	}
	for _, version := range caps.Versions {
		if version == 1 {
			for _, requiresEnabled := range nonEmpty {
				if requiresEnabled && !caps.Enabled {
					return errors.New("new Application Gateway access policies are not enabled on this platform; explicit empty-object resets remain available")
				}
			}
			return nil
		}
	}
	return errors.New("this platform does not advertise Application Gateway access policies version 1")
}

// The protocol capability remains available for reset even during a rollout
// rollback. Only nonempty policy documents require activation of the feature.
func appGWPoliciesNonEmpty(policies ...any) bool {
	for _, policy := range policies {
		raw, err := json.Marshal(policy)
		if err != nil || (string(raw) != "null" && string(raw) != "{}") {
			return true
		}
	}
	return false
}

// Independent Terraform resources share a client. Serialize the revision read
// and write per gateway, without holding a lock during asynchronous convergence.
func (c *Client) gatewayMutationLock(ctx context.Context, id string) (func(), error) {
	lockValue, _ := c.appgwPolicyLocks.LoadOrStore(id, make(chan struct{}, 1))
	lock := lockValue.(chan struct{})
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	unlock := func() { <-lock }
	return unlock, nil
}
func (c *Client) policyWrite(ctx context.Context, id string, nonEmpty ...bool) (func(), int64, error) {
	unlock, err := c.gatewayMutationLock(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	if err := c.RequireAppGWPolicies(ctx, nonEmpty...); err != nil {
		unlock()
		return nil, 0, err
	}
	gw, err := c.GetApplicationGateway(ctx, id)
	if err != nil {
		unlock()
		return nil, 0, AppGWPolicyError(err)
	}
	if gw.AccessPolicyGeneration == nil {
		unlock()
		return nil, 0, errors.New("platform did not return the access policy revision")
	}
	return unlock, *gw.AccessPolicyGeneration, nil
}
func (c *Client) UpdateAppGWListener(ctx context.Context, appgwID, listenerID string, req AppGWListenerUpdateRequest) (*AppGWListener, error) {
	unlock, revision, err := c.policyWrite(ctx, appgwID, appGWPoliciesNonEmpty(req.L7Policy))
	if err != nil {
		return nil, err
	}
	defer unlock()
	req.SecurityRevision = &revision
	var out AppGWListener
	if err := c.do(ctx, http.MethodPatch, "/v1/app-gateways/"+appgwID+"/listeners/"+listenerID, req, &out); err != nil {
		return nil, AppGWPolicyError(err)
	}
	return &out, nil
}

// A resource being active says nothing about its access policy. Wait for all
// expected replicas and drained connections of the latest requested generation.
func (c *Client) WaitAppGWAccessPolicy(ctx context.Context, id string, generation int64, timeout time.Duration) (*ApplicationGateway, error) {
	return c.waitAppGWAccessPolicy(ctx, id, generation, timeout, nil)
}
func (c *Client) waitAppGWAccessPolicy(ctx context.Context, id string, generation int64, timeout time.Duration, extraReady func(*ApplicationGateway) bool) (*ApplicationGateway, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		gw, err := c.GetApplicationGateway(ctx, id)
		if err != nil {
			return nil, AppGWPolicyError(err)
		}
		state := gw.AccessPolicyStatus
		if gw.AccessPolicyGeneration == nil || state == nil {
			return gw, errors.New("platform did not confirm access policy application")
		}
		ready := *gw.AccessPolicyGeneration >= generation && state.State == "applied" && state.Generation == *gw.AccessPolicyGeneration && len(state.Replicas) == int(GatewayReplicaCount(gw.ReplicaCount))
		for _, role := range []string{"primary", "secondary"} {
			if role == "secondary" && GatewayReplicaCount(gw.ReplicaCount) == 1 {
				continue
			}
			replica, ok := state.Replicas[role]
			ready = ready && ok && replica.State == "applied" && replica.Generation == state.Generation && replica.RevocationComplete
		}
		if ready && (extraReady == nil || extraReady(gw)) {
			return gw, nil
		}
		if gw.Status == AppGWStatusDeleting || gw.Status == AppGWStatusError {
			return gw, errors.New("gateway cannot confirm the requested access policy")
		}
		select {
		case <-ctx.Done():
			return gw, errors.New("access policy application remains unconfirmed; refresh the resource before retrying")
		case <-time.After(2 * time.Second):
		}
	}
}

// A protected child remains in Terraform state until its removal is effective
// on every replica. The revision comes from the committed DELETE response,
// never from a possibly lagging read plus one.
func (c *Client) deleteAppGWChild(ctx context.Context, appgwID, childID, collection string, knownRevision []int64) error {
	unlock, err := c.gatewayMutationLock(ctx, appgwID)
	if err != nil {
		return err
	}
	var headers http.Header
	err = c.do(ctx, http.MethodDelete, "/v1/app-gateways/"+appgwID+"/"+collection+"/"+childID, nil, nil, &headers)
	unlock()
	minimum := int64(0)
	for _, known := range knownRevision {
		if known > minimum {
			minimum = known
		}
	}
	if err != nil && (!IsNotFound(err) || minimum == 0) {
		return err
	}
	if err == nil {
		raw := headers.Get("X-AppGW-Security-Revision")
		if raw == "" {
			if minimum > 0 {
				return errors.New("platform did not confirm the deletion policy revision; refresh before retrying")
			}
			return nil // Old platforms have no access-policy contract.
		}
		revision, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || revision < minimum {
			return errors.New("platform returned an invalid deletion policy revision")
		}
		minimum = revision
	}
	if minimum == 0 {
		return nil
	}
	_, err = c.waitAppGWAccessPolicy(ctx, appgwID, minimum, 5*time.Minute, func(gw *ApplicationGateway) bool {
		if collection == "listeners" {
			for _, child := range gw.Listeners {
				if child.ID == childID {
					return false
				}
			}
		} else {
			for _, child := range gw.Routes {
				if child.ID == childID {
					return false
				}
			}
		}
		return true
	})
	return err
}
