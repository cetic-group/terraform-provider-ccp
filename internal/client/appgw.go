// Application Gateway v1 client wrappers.
//
// Endpoints under `/v1/app-gateways/...`. Mutation calls trigger an async
// HAProxy regen on the LXC pair — provider Read() must poll the gateway
// `status` field for stabilisation.
package client

import (
	"context"
	"fmt"
	"net/http"
)

// ─── Application Gateway ─────────────────────────────────────────────────────

func (c *Client) ListApplicationGateways(ctx context.Context, region string) ([]ApplicationGateway, error) {
	path := "/v1/app-gateways"
	if region != "" {
		path += "?region=" + region
	}
	var out []ApplicationGateway
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetApplicationGateway(ctx context.Context, id string) (*ApplicationGateway, error) {
	var out ApplicationGateway
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateApplicationGateway(ctx context.Context, req ApplicationGatewayCreateRequest) (*ApplicationGateway, error) {
	var out ApplicationGateway
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateApplicationGateway(ctx context.Context, id string, req ApplicationGatewayUpdateRequest) (*ApplicationGateway, error) {
	var out ApplicationGateway
	if err := c.do(ctx, http.MethodPatch, "/v1/app-gateways/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteApplicationGateway(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/app-gateways/"+id, nil, nil)
}

func (c *Client) AttachApplicationGatewayPublicIP(ctx context.Context, id string, req ApplicationGatewayAttachIPRequest) (*ApplicationGateway, error) {
	var out ApplicationGateway
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+id+"/attach-ip", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DetachApplicationGatewayPublicIP(ctx context.Context, id string) (*ApplicationGateway, error) {
	var out ApplicationGateway
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+id+"/detach-ip", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Listener ────────────────────────────────────────────────────────────────

func (c *Client) ListAppGWListeners(ctx context.Context, appgwID string) ([]AppGWListener, error) {
	var out []AppGWListener
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+appgwID+"/listeners", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAppGWListener — the API exposes single-listener GET; if not available
// we fall back to list-and-filter at call site. For consistency with other
// resources we expose a single-entity wrapper here.
func (c *Client) GetAppGWListener(ctx context.Context, appgwID, listenerID string) (*AppGWListener, error) {
	list, err := c.ListAppGWListeners(ctx, appgwID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == listenerID {
			return &list[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: fmt.Sprintf("/v1/app-gateways/%s/listeners/%s", appgwID, listenerID), Detail: "appgw listener not found"}
}

func (c *Client) CreateAppGWListener(ctx context.Context, appgwID string, req AppGWListenerCreateRequest) (*AppGWListener, error) {
	var out AppGWListener
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+appgwID+"/listeners", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteAppGWListener(ctx context.Context, appgwID, listenerID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/app-gateways/"+appgwID+"/listeners/"+listenerID, nil, nil)
}

func (c *Client) RenewAppGWListenerCert(ctx context.Context, appgwID, listenerID string) (*AppGWListener, error) {
	var out AppGWListener
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+appgwID+"/listeners/"+listenerID+"/renew-cert", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Target Group ────────────────────────────────────────────────────────────

func (c *Client) ListAppGWTargetGroups(ctx context.Context, appgwID string) ([]AppGWTargetGroup, error) {
	var out []AppGWTargetGroup
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+appgwID+"/target-groups", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetAppGWTargetGroup(ctx context.Context, appgwID, tgID string) (*AppGWTargetGroup, error) {
	var out AppGWTargetGroup
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateAppGWTargetGroup(ctx context.Context, appgwID string, req AppGWTargetGroupCreateRequest) (*AppGWTargetGroup, error) {
	var out AppGWTargetGroup
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+appgwID+"/target-groups", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateAppGWTargetGroup(ctx context.Context, appgwID, tgID string, req AppGWTargetGroupUpdateRequest) (*AppGWTargetGroup, error) {
	var out AppGWTargetGroup
	if err := c.do(ctx, http.MethodPatch, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteAppGWTargetGroup(ctx context.Context, appgwID, tgID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID, nil, nil)
}

// ─── Target Group Member ─────────────────────────────────────────────────────

func (c *Client) ListAppGWTargetGroupMembers(ctx context.Context, appgwID, tgID string) ([]AppGWTargetGroupMember, error) {
	var out []AppGWTargetGroupMember
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID+"/members", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetAppGWTargetGroupMember(ctx context.Context, appgwID, tgID, memberID string) (*AppGWTargetGroupMember, error) {
	list, err := c.ListAppGWTargetGroupMembers(ctx, appgwID, tgID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == memberID {
			return &list[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: fmt.Sprintf("/v1/app-gateways/%s/target-groups/%s/members/%s", appgwID, tgID, memberID), Detail: "appgw target group member not found"}
}

func (c *Client) AddAppGWTargetGroupMember(ctx context.Context, appgwID, tgID string, req AppGWTargetGroupMemberCreateRequest) (*AppGWTargetGroupMember, error) {
	var out AppGWTargetGroupMember
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID+"/members", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateAppGWTargetGroupMember(ctx context.Context, appgwID, tgID, memberID string, req AppGWTargetGroupMemberUpdateRequest) (*AppGWTargetGroupMember, error) {
	var out AppGWTargetGroupMember
	if err := c.do(ctx, http.MethodPatch, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID+"/members/"+memberID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RemoveAppGWTargetGroupMember(ctx context.Context, appgwID, tgID, memberID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/app-gateways/"+appgwID+"/target-groups/"+tgID+"/members/"+memberID, nil, nil)
}

// ─── Route ───────────────────────────────────────────────────────────────────

func (c *Client) ListAppGWRoutes(ctx context.Context, appgwID string) ([]AppGWRoute, error) {
	var out []AppGWRoute
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+appgwID+"/routes", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetAppGWRoute(ctx context.Context, appgwID, routeID string) (*AppGWRoute, error) {
	var out AppGWRoute
	if err := c.do(ctx, http.MethodGet, "/v1/app-gateways/"+appgwID+"/routes/"+routeID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateAppGWRoute(ctx context.Context, appgwID string, req AppGWRouteCreateRequest) (*AppGWRoute, error) {
	var out AppGWRoute
	if err := c.do(ctx, http.MethodPost, "/v1/app-gateways/"+appgwID+"/routes", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateAppGWRoute(ctx context.Context, appgwID, routeID string, req AppGWRouteUpdateRequest) (*AppGWRoute, error) {
	var out AppGWRoute
	if err := c.do(ctx, http.MethodPatch, "/v1/app-gateways/"+appgwID+"/routes/"+routeID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteAppGWRoute(ctx context.Context, appgwID, routeID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/app-gateways/"+appgwID+"/routes/"+routeID, nil, nil)
}
