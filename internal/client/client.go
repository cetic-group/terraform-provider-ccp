// Package client wraps the CETIC Cloud REST API.
//
// All endpoints accept a Bearer API key (ccp_live_*) or JWT. Resources are
// scoped server-side via the auth context (org_id) — there is no tenant_id
// in request bodies.
//
// Errors from the API follow FastAPI convention: `{"detail": "message"}`.
// We unwrap that into a typed APIError so callers can switch on status code.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://api.cloud.cetic-group.com"
	defaultTimeout  = 30 * time.Second
	userAgent       = "terraform-provider-ccp"
)

// Client is the typed CETIC Cloud REST client.
type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// New builds a Client. Endpoint should be the base URL without /v1 suffix
// (e.g. "https://api.cloud.cetic-group.com"). The client will append /v1 itself.
//
// apiKey must be a `ccp_live_...` API key. JWTs are also accepted but the
// provider only documents API keys.
func New(endpoint, apiKey, version string) *Client {
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")
	ua := userAgent
	if version != "" {
		ua = fmt.Sprintf("%s/%s", userAgent, version)
	}
	return &Client{
		endpoint:   endpoint,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  ua,
	}
}

// APIError is returned for non-2xx responses with the parsed FastAPI detail.
type APIError struct {
	StatusCode int
	Detail     string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("%s %s: HTTP %d — %s", e.Method, e.Path, e.StatusCode, e.Detail)
}

// IsNotFound reports whether err is an APIError with status 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsConflict reports whether err is an APIError with status 409.
func IsConflict(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// do issues a request and decodes the JSON response into out. If out is nil,
// the body is discarded. Non-2xx returns *APIError.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	url := c.endpoint + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseAPIError(resp, method, path)
	}

	// 204 No Content — nothing to decode
	if resp.StatusCode == http.StatusNoContent || out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func parseAPIError(resp *http.Response, method, path string) error {
	body, _ := io.ReadAll(resp.Body)
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Method:     method,
		Path:       path,
	}
	// FastAPI: {"detail": "..."} — but detail can also be a list of validation errors
	var parsed struct {
		Detail any `json:"detail"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Detail != nil {
		switch d := parsed.Detail.(type) {
		case string:
			apiErr.Detail = d
		default:
			// Validation error array — re-encode for visibility
			if raw, e := json.Marshal(d); e == nil {
				apiErr.Detail = string(raw)
			}
		}
	} else if len(body) > 0 {
		apiErr.Detail = strings.TrimSpace(string(body))
	}
	return apiErr
}

// ─── SSH Keys ────────────────────────────────────────────────────────────────

func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	var keys []SSHKey
	if err := c.do(ctx, http.MethodGet, "/v1/ssh-keys", nil, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (c *Client) CreateSSHKey(ctx context.Context, req SSHKeyCreateRequest) (*SSHKey, error) {
	var out SSHKey
	if err := c.do(ctx, http.MethodPost, "/v1/ssh-keys", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/ssh-keys/"+id, nil, nil)
}

// GetSSHKey fetches a single SSH key by ID. The API has no GET single endpoint
// so we list-and-filter. Returns *APIError(404) if not found.
func (c *Client) GetSSHKey(ctx context.Context, id string) (*SSHKey, error) {
	keys, err := c.ListSSHKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/v1/ssh-keys/" + id, Detail: "ssh key not found"}
}

// ─── VPCs ────────────────────────────────────────────────────────────────────

func (c *Client) ListVPCs(ctx context.Context) ([]VPC, error) {
	var vpcs []VPC
	if err := c.do(ctx, http.MethodGet, "/v1/vpcs", nil, &vpcs); err != nil {
		return nil, err
	}
	return vpcs, nil
}

func (c *Client) GetVPC(ctx context.Context, id string) (*VPC, error) {
	var out VPC
	if err := c.do(ctx, http.MethodGet, "/v1/vpcs/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateVPC(ctx context.Context, req VPCCreateRequest) (*VPC, error) {
	var out VPC
	if err := c.do(ctx, http.MethodPost, "/v1/vpcs", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVPC(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vpcs/"+id, nil, nil)
}

// ─── VNets ───────────────────────────────────────────────────────────────────

func (c *Client) ListVNets(ctx context.Context, vpcID string) ([]VNet, error) {
	var vnets []VNet
	if err := c.do(ctx, http.MethodGet, "/v1/vpcs/"+vpcID+"/vnets", nil, &vnets); err != nil {
		return nil, err
	}
	return vnets, nil
}

// GetVNet fetches a single VNet via list-and-filter (no GET single endpoint).
func (c *Client) GetVNet(ctx context.Context, vpcID, vnetID string) (*VNet, error) {
	vnets, err := c.ListVNets(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	for i := range vnets {
		if vnets[i].ID == vnetID {
			return &vnets[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: fmt.Sprintf("/v1/vpcs/%s/vnets/%s", vpcID, vnetID), Detail: "vnet not found"}
}

func (c *Client) CreateVNet(ctx context.Context, vpcID string, req VNetCreateRequest) (*VNet, error) {
	var out VNet
	if err := c.do(ctx, http.MethodPost, "/v1/vpcs/"+vpcID+"/vnets", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateVNet(ctx context.Context, vpcID, vnetID string, req VNetUpdateRequest) (*VNet, error) {
	var out VNet
	if err := c.do(ctx, http.MethodPatch, "/v1/vpcs/"+vpcID+"/vnets/"+vnetID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVNet(ctx context.Context, vpcID, vnetID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vpcs/"+vpcID+"/vnets/"+vnetID, nil, nil)
}

// ─── Regions ─────────────────────────────────────────────────────────────────

func (c *Client) ListRegions(ctx context.Context) ([]Region, error) {
	var regions []Region
	if err := c.do(ctx, http.MethodGet, "/v1/regions", nil, &regions); err != nil {
		return nil, err
	}
	return regions, nil
}

// ─── Containers ──────────────────────────────────────────────────────────────

func (c *Client) ListContainers(ctx context.Context, region string) ([]Container, error) {
	path := "/v1/containers"
	if region != "" {
		path += "?region=" + region
	}
	var out []Container
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetContainer(ctx context.Context, id string) (*Container, error) {
	var out Container
	if err := c.do(ctx, http.MethodGet, "/v1/containers/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateContainer(ctx context.Context, req ContainerCreateRequest) (*Container, error) {
	var out Container
	if err := c.do(ctx, http.MethodPost, "/v1/containers", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/containers/"+id, nil, nil)
}

// ContainerAction posts a state transition: start | stop | restart.
func (c *Client) ContainerAction(ctx context.Context, id, action string) error {
	return c.do(ctx, http.MethodPost, "/v1/containers/"+id+"/actions",
		ContainerActionRequest{Action: action}, nil)
}

// ─── Block Volumes ───────────────────────────────────────────────────────────

func (c *Client) ListBlockVolumes(ctx context.Context, region string) ([]BlockVolume, error) {
	path := "/v1/volumes"
	if region != "" {
		path += "?region=" + region
	}
	var out []BlockVolume
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetBlockVolume(ctx context.Context, id string) (*BlockVolume, error) {
	var out BlockVolume
	if err := c.do(ctx, http.MethodGet, "/v1/volumes/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateBlockVolume(ctx context.Context, req BlockVolumeCreateRequest) (*BlockVolume, error) {
	var out BlockVolume
	if err := c.do(ctx, http.MethodPost, "/v1/volumes", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteBlockVolume(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/volumes/"+id, nil, nil)
}

func (c *Client) AttachBlockVolume(ctx context.Context, id string, req BlockVolumeAttachRequest) (*BlockVolume, error) {
	var out BlockVolume
	if err := c.do(ctx, http.MethodPost, "/v1/volumes/"+id+"/attach", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DetachBlockVolume(ctx context.Context, id string) (*BlockVolume, error) {
	var out BlockVolume
	if err := c.do(ctx, http.MethodPost, "/v1/volumes/"+id+"/detach", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResizeBlockVolume(ctx context.Context, id string, sizeGB int) (*BlockVolume, error) {
	var out BlockVolume
	if err := c.do(ctx, http.MethodPost, "/v1/volumes/"+id+"/resize",
		BlockVolumeResizeRequest{SizeGB: sizeGB}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Public IPs ──────────────────────────────────────────────────────────────

func (c *Client) ListPublicIPs(ctx context.Context) ([]PublicIP, error) {
	var out []PublicIP
	if err := c.do(ctx, http.MethodGet, "/v1/public-ips", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPublicIP — list+filter (no GET single endpoint).
func (c *Client) GetPublicIP(ctx context.Context, id string) (*PublicIP, error) {
	ips, err := c.ListPublicIPs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ips {
		if ips[i].ID == id {
			return &ips[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/v1/public-ips/" + id, Detail: "public ip not found"}
}

func (c *Client) AllocatePublicIP(ctx context.Context, req PublicIPAllocateRequest) (*PublicIP, error) {
	var out PublicIP
	if err := c.do(ctx, http.MethodPost, "/v1/public-ips", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ReleasePublicIP(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/public-ips/"+id, nil, nil)
}

func (c *Client) AttachPublicIP(ctx context.Context, id string, req PublicIPAttachRequest) (*PublicIP, error) {
	var out PublicIP
	if err := c.do(ctx, http.MethodPost, "/v1/public-ips/"+id+"/attach", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DetachPublicIP(ctx context.Context, id string) (*PublicIP, error) {
	var out PublicIP
	if err := c.do(ctx, http.MethodPost, "/v1/public-ips/"+id+"/detach", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Object Buckets (Ceph RGW) ───────────────────────────────────────────────

func (c *Client) ListObjectBuckets(ctx context.Context, region string) ([]ObjectBucket, error) {
	path := "/v1/buckets"
	if region != "" {
		path += "?region=" + region
	}
	var out []ObjectBucket
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetObjectBucket(ctx context.Context, id string) (*ObjectBucket, error) {
	var out ObjectBucket
	if err := c.do(ctx, http.MethodGet, "/v1/buckets/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateObjectBucket(ctx context.Context, req ObjectBucketCreateRequest) (*ObjectBucket, error) {
	var out ObjectBucket
	if err := c.do(ctx, http.MethodPost, "/v1/buckets", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateObjectBucket(ctx context.Context, id string, req ObjectBucketUpdateRequest) (*ObjectBucket, error) {
	var out ObjectBucket
	if err := c.do(ctx, http.MethodPatch, "/v1/buckets/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteObjectBucket(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/buckets/"+id, nil, nil)
}

// GetObjectBucketCredentials returns the master S3 credentials for the
// tenant in the bucket's region (covers all buckets in that region).
// Requires bucket status == active. Sensitive — handle with care.
func (c *Client) GetObjectBucketCredentials(ctx context.Context, id string) (*ObjectBucketCredentials, error) {
	var out ObjectBucketCredentials
	if err := c.do(ctx, http.MethodGet, "/v1/buckets/"+id+"/credentials", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── VM Instances (QEMU) ─────────────────────────────────────────────────────

func (c *Client) ListVMInstances(ctx context.Context, region string) ([]VMInstance, error) {
	path := "/v1/vm-instances"
	if region != "" {
		path += "?region=" + region
	}
	var out []VMInstance
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetVMInstance(ctx context.Context, id string) (*VMInstance, error) {
	var out VMInstance
	if err := c.do(ctx, http.MethodGet, "/v1/vm-instances/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateVMInstance(ctx context.Context, req VMInstanceCreateRequest) (*VMInstance, error) {
	var out VMInstance
	if err := c.do(ctx, http.MethodPost, "/v1/vm-instances", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateVMInstance(ctx context.Context, id string, req VMInstanceUpdateRequest) (*VMInstance, error) {
	var out VMInstance
	if err := c.do(ctx, http.MethodPatch, "/v1/vm-instances/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVMInstance(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vm-instances/"+id, nil, nil)
}

// VMInstanceAction — action ∈ {start, stop, shutdown, reboot}.
func (c *Client) VMInstanceAction(ctx context.Context, id, action string) error {
	return c.do(ctx, http.MethodPost, "/v1/vm-instances/"+id+"/actions",
		VMActionRequest{Action: action}, nil)
}

// ─── Organizations ───────────────────────────────────────────────────────────

// ListOrganizations returns all organizations accessible to the current
// auth context (owned + invited). For an API key, this lists every org the
// key's tenant has access to — but the active org is determined server-side
// by `api_keys.org_id`.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var out []Organization
	if err := c.do(ctx, http.MethodGet, "/v1/orgs", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ─── Load Balancer (Phase 4) ─────────────────────────────────────────────────

func (c *Client) ListLoadBalancers(ctx context.Context, region string) ([]LoadBalancer, error) {
	path := "/v1/load-balancers"
	if region != "" {
		path += "?region=" + region
	}
	var out []LoadBalancer
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetLoadBalancer(ctx context.Context, id string) (*LoadBalancer, error) {
	var out LoadBalancer
	if err := c.do(ctx, http.MethodGet, "/v1/load-balancers/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateLoadBalancer(ctx context.Context, req LoadBalancerCreateRequest) (*LoadBalancer, error) {
	var out LoadBalancer
	if err := c.do(ctx, http.MethodPost, "/v1/load-balancers", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateLoadBalancer(ctx context.Context, id string, req LoadBalancerUpdateRequest) (*LoadBalancer, error) {
	var out LoadBalancer
	if err := c.do(ctx, http.MethodPatch, "/v1/load-balancers/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteLoadBalancer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/load-balancers/"+id, nil, nil)
}

func (c *Client) AttachLoadBalancerPublicIP(ctx context.Context, id string, req LoadBalancerAttachIPRequest) (*LoadBalancer, error) {
	var out LoadBalancer
	if err := c.do(ctx, http.MethodPost, "/v1/load-balancers/"+id+"/attach-ip", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DetachLoadBalancerPublicIP(ctx context.Context, id string) (*LoadBalancer, error) {
	var out LoadBalancer
	if err := c.do(ctx, http.MethodPost, "/v1/load-balancers/"+id+"/detach-ip", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, req LBListenerCreateRequest) (*LBListener, error) {
	var out LBListener
	if err := c.do(ctx, http.MethodPost, "/v1/load-balancers/"+lbID+"/listeners", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/load-balancers/"+lbID+"/listeners/"+listenerID, nil, nil)
}

func (c *Client) AddLBBackend(ctx context.Context, lbID, listenerID string, req LBBackendCreateRequest) (*LBBackend, error) {
	var out LBBackend
	if err := c.do(ctx, http.MethodPost, "/v1/load-balancers/"+lbID+"/listeners/"+listenerID+"/backends", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RemoveLBBackend(ctx context.Context, lbID, listenerID, backendID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/load-balancers/"+lbID+"/listeners/"+listenerID+"/backends/"+backendID, nil, nil)
}

// ─── Container Scale Set ─────────────────────────────────────────────────────

func (c *Client) ListContainerScaleSets(ctx context.Context, region string) ([]ContainerScaleSet, error) {
	path := "/v1/container-scale-sets"
	if region != "" {
		path += "?region=" + region
	}
	var out []ContainerScaleSet
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetContainerScaleSet(ctx context.Context, id string) (*ContainerScaleSet, error) {
	var out ContainerScaleSet
	if err := c.do(ctx, http.MethodGet, "/v1/container-scale-sets/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateContainerScaleSet(ctx context.Context, req ContainerScaleSetCreateRequest) (*ContainerScaleSet, error) {
	var out ContainerScaleSet
	if err := c.do(ctx, http.MethodPost, "/v1/container-scale-sets", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateContainerScaleSet(ctx context.Context, id string, req ContainerScaleSetUpdateRequest) (*ContainerScaleSet, error) {
	var out ContainerScaleSet
	if err := c.do(ctx, http.MethodPatch, "/v1/container-scale-sets/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteContainerScaleSet(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/container-scale-sets/"+id, nil, nil)
}

// ─── VM Scale Set ────────────────────────────────────────────────────────────

func (c *Client) ListVMScaleSets(ctx context.Context, region string) ([]VMScaleSet, error) {
	path := "/v1/vm-scale-sets"
	if region != "" {
		path += "?region=" + region
	}
	var out []VMScaleSet
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetVMScaleSet(ctx context.Context, id string) (*VMScaleSet, error) {
	var out VMScaleSet
	if err := c.do(ctx, http.MethodGet, "/v1/vm-scale-sets/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateVMScaleSet(ctx context.Context, req VMScaleSetCreateRequest) (*VMScaleSet, error) {
	var out VMScaleSet
	if err := c.do(ctx, http.MethodPost, "/v1/vm-scale-sets", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateVMScaleSet(ctx context.Context, id string, req VMScaleSetUpdateRequest) (*VMScaleSet, error) {
	var out VMScaleSet
	if err := c.do(ctx, http.MethodPatch, "/v1/vm-scale-sets/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVMScaleSet(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vm-scale-sets/"+id, nil, nil)
}

// ─── K8s Cluster (CLKS — Phase 6) ───────────────────────────────────────────

func (c *Client) ListK8sClusters(ctx context.Context, region string) ([]K8sCluster, error) {
	path := "/v1/k8s/clusters"
	if region != "" {
		path += "?region=" + region
	}
	var out []K8sCluster
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetK8sCluster(ctx context.Context, id string) (*K8sCluster, error) {
	var out K8sCluster
	if err := c.do(ctx, http.MethodGet, "/v1/k8s/clusters/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateK8sCluster(ctx context.Context, req K8sClusterCreateRequest) (*K8sCluster, error) {
	var out K8sCluster
	if err := c.do(ctx, http.MethodPost, "/v1/k8s/clusters", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateK8sCluster(ctx context.Context, id string, req K8sClusterUpdateRequest) (*K8sCluster, error) {
	var out K8sCluster
	if err := c.do(ctx, http.MethodPatch, "/v1/k8s/clusters/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteK8sCluster(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/k8s/clusters/"+id, nil, nil)
}

func (c *Client) GetK8sClusterKubeconfig(ctx context.Context, id string) (string, error) {
	var out struct {
		Kubeconfig string `json:"kubeconfig"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/k8s/clusters/"+id+"/kubeconfig", nil, &out); err != nil {
		return "", err
	}
	return out.Kubeconfig, nil
}

func (c *Client) UpgradeK8sClusterVersion(ctx context.Context, id string, req K8sUpgradeVersionRequest) (*K8sCluster, error) {
	var out K8sCluster
	if err := c.do(ctx, http.MethodPost, "/v1/k8s/clusters/"+id+"/upgrade-version", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AttachIPToK8sCluster(ctx context.Context, id string, req K8sAttachIPRequest) (*K8sCluster, error) {
	var out K8sCluster
	if err := c.do(ctx, http.MethodPost, "/v1/k8s/clusters/"+id+"/attach-ip", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DetachIPFromK8sCluster(ctx context.Context, id string) (*K8sCluster, error) {
	var out K8sCluster
	if err := c.do(ctx, http.MethodPost, "/v1/k8s/clusters/"+id+"/detach-ip", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── K8s Node Pool (sub-resource du cluster) ────────────────────────────────

func (c *Client) ListK8sNodePools(ctx context.Context, clusterID string) ([]K8sNodePool, error) {
	var out []K8sNodePool
	if err := c.do(ctx, http.MethodGet, "/v1/k8s/clusters/"+clusterID+"/node-pools", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetK8sNodePool(ctx context.Context, clusterID, poolID string) (*K8sNodePool, error) {
	var out K8sNodePool
	if err := c.do(ctx, http.MethodGet, "/v1/k8s/clusters/"+clusterID+"/node-pools/"+poolID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateK8sNodePool(ctx context.Context, clusterID string, req K8sNodePoolCreateRequest) (*K8sNodePool, error) {
	var out K8sNodePool
	if err := c.do(ctx, http.MethodPost, "/v1/k8s/clusters/"+clusterID+"/node-pools", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateK8sNodePool(ctx context.Context, clusterID, poolID string, req K8sNodePoolUpdateRequest) (*K8sNodePool, error) {
	var out K8sNodePool
	if err := c.do(ctx, http.MethodPatch, "/v1/k8s/clusters/"+clusterID+"/node-pools/"+poolID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteK8sNodePool(ctx context.Context, clusterID, poolID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/k8s/clusters/"+clusterID+"/node-pools/"+poolID, nil, nil)
}

// ─── DB PostgreSQL ──────────────────────────────────────────────────────────

func (c *Client) ListDbPg(ctx context.Context) ([]DbPgInstance, error) {
	var out []DbPgInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/pg", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetDbPg(ctx context.Context, id string) (*DbPgInstance, error) {
	var out DbPgInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/pg/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateDbPg(ctx context.Context, req DbPgInstanceCreateRequest) (*DbPgInstance, error) {
	var out DbPgInstance
	if err := c.do(ctx, http.MethodPost, "/v1/db/pg", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteDbPg(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/db/pg/"+id, nil, nil)
}

// ─── DB Valkey ──────────────────────────────────────────────────────────────

func (c *Client) ListDbValkey(ctx context.Context) ([]DbValkeyInstance, error) {
	var out []DbValkeyInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/valkey", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetDbValkey(ctx context.Context, id string) (*DbValkeyInstance, error) {
	var out DbValkeyInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/valkey/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateDbValkey(ctx context.Context, req DbValkeyInstanceCreateRequest) (*DbValkeyInstance, error) {
	var out DbValkeyInstance
	if err := c.do(ctx, http.MethodPost, "/v1/db/valkey", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteDbValkey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/db/valkey/"+id, nil, nil)
}

// ─── DB MariaDB ─────────────────────────────────────────────────────────────

func (c *Client) ListDbMysql(ctx context.Context) ([]DbMysqlInstance, error) {
	var out []DbMysqlInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/mysql", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetDbMysql(ctx context.Context, id string) (*DbMysqlInstance, error) {
	var out DbMysqlInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/mysql/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateDbMysql(ctx context.Context, req DbMysqlInstanceCreateRequest) (*DbMysqlInstance, error) {
	var out DbMysqlInstance
	if err := c.do(ctx, http.MethodPost, "/v1/db/mysql", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteDbMysql(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/db/mysql/"+id, nil, nil)
}

// ─── DB FerretDB ────────────────────────────────────────────────────────────

func (c *Client) ListDbFerretdb(ctx context.Context) ([]DbFerretdbInstance, error) {
	var out []DbFerretdbInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/ferretdb", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetDbFerretdb(ctx context.Context, id string) (*DbFerretdbInstance, error) {
	var out DbFerretdbInstance
	if err := c.do(ctx, http.MethodGet, "/v1/db/ferretdb/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateDbFerretdb(ctx context.Context, req DbFerretdbInstanceCreateRequest) (*DbFerretdbInstance, error) {
	var out DbFerretdbInstance
	if err := c.do(ctx, http.MethodPost, "/v1/db/ferretdb", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteDbFerretdb(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/db/ferretdb/"+id, nil, nil)
}

// ─── Organization ──────────────────────────────────────────────────────────

func (c *Client) GetOrganization(ctx context.Context, id string) (*OrganizationResource, error) {
	var out OrganizationResource
	if err := c.do(ctx, http.MethodGet, "/v1/orgs/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateOrganization(ctx context.Context, req OrganizationCreateRequest) (*OrganizationResource, error) {
	var out OrganizationResource
	if err := c.do(ctx, http.MethodPost, "/v1/orgs", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateOrganization(ctx context.Context, id string, req OrganizationUpdateRequest) (*OrganizationResource, error) {
	var out OrganizationResource
	if err := c.do(ctx, http.MethodPatch, "/v1/orgs/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteOrganization(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/orgs/"+id, nil, nil)
}

// ─── API Key ───────────────────────────────────────────────────────────────

func (c *Client) GetApiKey(ctx context.Context, id string) (*ApiKey, error) {
	var out ApiKey
	if err := c.do(ctx, http.MethodGet, "/v1/api-keys/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateApiKey(ctx context.Context, req ApiKeyCreateRequest) (*ApiKeyCreateResponse, error) {
	var out ApiKeyCreateResponse
	if err := c.do(ctx, http.MethodPost, "/v1/api-keys", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteApiKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/api-keys/"+id, nil, nil)
}

// ─── Org Member ────────────────────────────────────────────────────────────

func (c *Client) GetOrgMember(ctx context.Context, id string) (*OrgMember, error) {
	var out OrgMember
	if err := c.do(ctx, http.MethodGet, "/v1/members/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateOrgMember(ctx context.Context, req OrgMemberCreateRequest) (*OrgMember, error) {
	var out OrgMember
	if err := c.do(ctx, http.MethodPost, "/v1/members", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateOrgMember(ctx context.Context, id string, req OrgMemberUpdateRequest) (*OrgMember, error) {
	var out OrgMember
	if err := c.do(ctx, http.MethodPatch, "/v1/members/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteOrgMember(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/members/"+id, nil, nil)
}

// ─── VNet Peering ──────────────────────────────────────────────────────────

func (c *Client) GetVnetPeering(ctx context.Context, id string) (*VnetPeering, error) {
	var out VnetPeering
	if err := c.do(ctx, http.MethodGet, "/v1/vnet-peerings/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateVnetPeering(ctx context.Context, req VnetPeeringCreateRequest) (*VnetPeering, error) {
	var out VnetPeering
	if err := c.do(ctx, http.MethodPost, "/v1/vnet-peerings", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVnetPeering(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vnet-peerings/"+id, nil, nil)
}

// ─── VPC Peering ───────────────────────────────────────────────────────────

func (c *Client) GetVpcPeering(ctx context.Context, vpcID, peeringID string) (*VpcPeering, error) {
	var out VpcPeering
	if err := c.do(ctx, http.MethodGet, "/v1/vpcs/"+vpcID+"/peerings/"+peeringID, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateVpcPeering(ctx context.Context, vpcID string, req VpcPeeringCreateRequest) (*VpcPeering, error) {
	var out VpcPeering
	if err := c.do(ctx, http.MethodPost, "/v1/vpcs/"+vpcID+"/peerings", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteVpcPeering(ctx context.Context, vpcID, peeringID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vpcs/"+vpcID+"/peerings/"+peeringID, nil, nil)
}

// ─── Support Ticket ────────────────────────────────────────────────────────

func (c *Client) GetSupportTicket(ctx context.Context, id string) (*SupportTicket, error) {
	var out SupportTicket
	if err := c.do(ctx, http.MethodGet, "/v1/support/tickets/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateSupportTicket(ctx context.Context, req SupportTicketCreateRequest) (*SupportTicket, error) {
	var out SupportTicket
	if err := c.do(ctx, http.MethodPost, "/v1/support/tickets", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Object Storage Keys ──────────────────────────────────────────────────

func (c *Client) CreateObjectStorageKey(ctx context.Context, req ObjectStorageKeyCreateRequest) (*ObjectStorageKey, error) {
	var out ObjectStorageKey
	if err := c.do(ctx, http.MethodPost, "/v1/object-storage/keys", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetObjectStorageKey(ctx context.Context, id string) (*ObjectStorageKey, error) {
	var out ObjectStorageKey
	if err := c.do(ctx, http.MethodGet, "/v1/object-storage/keys/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteObjectStorageKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/object-storage/keys/"+id, nil, nil)
}

// ─── IPaaS Pool (admin) ────────────────────────────────────────────────────

func (c *Client) GetIpaasPool(ctx context.Context, id string) (*IpaasPool, error) {
	var out IpaasPool
	if err := c.do(ctx, http.MethodGet, "/v1/admin/ipaas/pools/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateIpaasPool(ctx context.Context, req IpaasPoolCreateRequest) (*IpaasPool, error) {
	var out IpaasPool
	if err := c.do(ctx, http.MethodPost, "/v1/admin/ipaas/pools", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateIpaasPool(ctx context.Context, id string, req IpaasPoolUpdateRequest) (*IpaasPool, error) {
	var out IpaasPool
	if err := c.do(ctx, http.MethodPatch, "/v1/admin/ipaas/pools/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteIpaasPool(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/admin/ipaas/pools/"+id, nil, nil)
}

// ─── Quota Request ─────────────────────────────────────────────────────────

func (c *Client) GetQuotaRequest(ctx context.Context, id string) (*QuotaRequest, error) {
	var out QuotaRequest
	if err := c.do(ctx, http.MethodGet, "/v1/quotas/requests/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateQuotaRequest(ctx context.Context, req QuotaRequestCreateRequest) (*QuotaRequest, error) {
	var out QuotaRequest
	if err := c.do(ctx, http.MethodPost, "/v1/quotas/requests", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ─── Container Snapshots ─────────────────────────────────────────────────────

func (c *Client) ListContainerSnapshots(ctx context.Context, containerID string) ([]ContainerSnapshot, error) {
	var out []ContainerSnapshot
	if err := c.do(ctx, http.MethodGet, "/v1/containers/"+containerID+"/snapshots", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateContainerSnapshot(ctx context.Context, containerID string, req ContainerSnapshotCreateRequest) (*ContainerSnapshot, error) {
	var out ContainerSnapshot
	if err := c.do(ctx, http.MethodPost, "/v1/containers/"+containerID+"/snapshots", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetContainerSnapshot(ctx context.Context, containerID, snapshotID string) (*ContainerSnapshot, error) {
	snaps, err := c.ListContainerSnapshots(ctx, containerID)
	if err != nil {
		return nil, err
	}
	for i := range snaps {
		if snaps[i].ID == snapshotID {
			return &snaps[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: "GET", Path: "/v1/containers/" + containerID + "/snapshots/" + snapshotID, Detail: "snapshot not found"}
}

func (c *Client) DeleteContainerSnapshot(ctx context.Context, containerID, snapshotID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/containers/"+containerID+"/snapshots/"+snapshotID, nil, nil)
}

// ─── VM Snapshots ─────────────────────────────────────────────────────────────

func (c *Client) ListVmSnapshots(ctx context.Context, vmID string) ([]VmSnapshot, error) {
	var out []VmSnapshot
	if err := c.do(ctx, http.MethodGet, "/v1/vms/"+vmID+"/snapshots", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateVmSnapshot(ctx context.Context, vmID string, req VmSnapshotCreateRequest) (*VmSnapshot, error) {
	var out VmSnapshot
	if err := c.do(ctx, http.MethodPost, "/v1/vms/"+vmID+"/snapshots", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVmSnapshot(ctx context.Context, vmID, snapshotID string) (*VmSnapshot, error) {
	snaps, err := c.ListVmSnapshots(ctx, vmID)
	if err != nil {
		return nil, err
	}
	for i := range snaps {
		if snaps[i].ID == snapshotID {
			return &snaps[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/v1/vms/" + vmID + "/snapshots/" + snapshotID, Detail: "vm snapshot not found"}
}

func (c *Client) DeleteVmSnapshot(ctx context.Context, vmID, snapshotID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vms/"+vmID+"/snapshots/"+snapshotID, nil, nil)
}

// ─── VNet IP Reservations ────────────────────────────────────────────────────

func (c *Client) ListVnetIpReservations(ctx context.Context, vnetID string) ([]VnetIpReservation, error) {
	var out []VnetIpReservation
	if err := c.do(ctx, http.MethodGet, "/v1/vnets/"+vnetID+"/ip-reservations", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateVnetIpReservation(ctx context.Context, vnetID string, req VnetIpReservationCreateRequest) (*VnetIpReservation, error) {
	var out VnetIpReservation
	if err := c.do(ctx, http.MethodPost, "/v1/vnets/"+vnetID+"/ip-reservations", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVnetIpReservation(ctx context.Context, vnetID, reservationID string) (*VnetIpReservation, error) {
	items, err := c.ListVnetIpReservations(ctx, vnetID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == reservationID {
			return &items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/v1/vnets/" + vnetID + "/ip-reservations/" + reservationID, Detail: "vnet ip reservation not found"}
}

func (c *Client) DeleteVnetIpReservation(ctx context.Context, vnetID, reservationID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vnets/"+vnetID+"/ip-reservations/"+reservationID, nil, nil)
}

// ─── VNet Firewall Rules ─────────────────────────────────────────────────────

func (c *Client) ListVnetFirewallRules(ctx context.Context, vnetID string) ([]VnetFirewallRule, error) {
	var out []VnetFirewallRule
	if err := c.do(ctx, http.MethodGet, "/v1/vnets/"+vnetID+"/firewall/rules", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CreateVnetFirewallRule(ctx context.Context, vnetID string, req VnetFirewallRuleCreateRequest) (*VnetFirewallRule, error) {
	var out VnetFirewallRule
	if err := c.do(ctx, http.MethodPost, "/v1/vnets/"+vnetID+"/firewall/rules", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVnetFirewallRule(ctx context.Context, vnetID, ruleID string) (*VnetFirewallRule, error) {
	rules, err := c.ListVnetFirewallRules(ctx, vnetID)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == ruleID {
			return &rules[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Method: http.MethodGet, Path: "/v1/vnets/" + vnetID + "/firewall/rules/" + ruleID, Detail: "vnet firewall rule not found"}
}

func (c *Client) UpdateVnetFirewallRule(ctx context.Context, vnetID, ruleID string, patch map[string]any) (*VnetFirewallRule, error) {
	var out VnetFirewallRule
	err := c.do(ctx, http.MethodPatch, "/v1/vnets/"+vnetID+"/firewall/rules/"+ruleID, patch, &out)
	return &out, err
}

func (c *Client) DeleteVnetFirewallRule(ctx context.Context, vnetID, ruleID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/vnets/"+vnetID+"/firewall/rules/"+ruleID, nil, nil)
}

// ─── Polling helper ──────────────────────────────────────────────────────────

// PollFunc returns (done, err). When done is true OR err is non-nil, polling stops.
type PollFunc func(ctx context.Context) (bool, error)

// Poll runs fn every interval until it returns done=true, or until timeout.
// Used for VPC/VNet status polling after create.
func Poll(ctx context.Context, interval, timeout time.Duration, fn PollFunc) error {
	deadline := time.Now().Add(timeout)
	for {
		done, err := fn(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("polling timeout after %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ListLxcTemplates returns the LXC template catalog (active templates only).
// GET /v1/templates — no auth required (public catalog).
func (c *Client) ListLxcTemplates(ctx context.Context) ([]LxcTemplate, error) {
	var out []LxcTemplate
	if err := c.do(ctx, http.MethodGet, "/v1/templates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListQemuTemplates returns the QEMU/VM template catalog (filtered to client-usable
// templates, excludes ccks-* internal kubernetes images).
// GET /v1/qemu-templates — no auth required.
func (c *Client) ListQemuTemplates(ctx context.Context) ([]QemuTemplate, error) {
	var out []QemuTemplate
	if err := c.do(ctx, http.MethodGet, "/v1/qemu-templates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDbPlans returns the DB plan catalog. If engine != "", filters to that engine.
// GET /v1/db/plans?engine=<engine>
func (c *Client) ListDbPlans(ctx context.Context, engine string) ([]DbPlan, error) {
	path := "/v1/db/plans"
	if engine != "" {
		path += "?engine=" + url.QueryEscape(engine)
	}
	var out []DbPlan
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDbEngineVersions returns the DB engine version catalog. If engine != "",
// filters to that engine.
// GET /v1/db/engine-versions?engine=<engine>
func (c *Client) ListDbEngineVersions(ctx context.Context, engine string) ([]DbEngineVersion, error) {
	path := "/v1/db/engine-versions"
	if engine != "" {
		path += "?engine=" + url.QueryEscape(engine)
	}
	var out []DbEngineVersion
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListK8sTemplates returns the K8s node OS template catalog.
// GET /v1/k8s/templates
func (c *Client) ListK8sTemplates(ctx context.Context) ([]K8sTemplate, error) {
	var out []K8sTemplate
	if err := c.do(ctx, http.MethodGet, "/v1/k8s/templates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCustomTemplates returns the tenant's custom templates.
// GET /v1/custom-templates
func (c *Client) ListCustomTemplates(ctx context.Context) ([]CustomTemplate, error) {
	var out []CustomTemplate
	if err := c.do(ctx, http.MethodGet, "/v1/custom-templates", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCustomTemplate returns a single custom template by id.
// GET /v1/custom-templates/{id}
func (c *Client) GetCustomTemplate(ctx context.Context, id string) (*CustomTemplate, error) {
	var out CustomTemplate
	if err := c.do(ctx, http.MethodGet, "/v1/custom-templates/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateCustomTemplateFromContainer triggers async snapshot of the given container
// into a new custom template.
// POST /v1/custom-templates/from-container/{container_id}
func (c *Client) CreateCustomTemplateFromContainer(ctx context.Context, containerID string, req CustomTemplateCreateRequest) (*CustomTemplate, error) {
	var out CustomTemplate
	if err := c.do(ctx, http.MethodPost, "/v1/custom-templates/from-container/"+containerID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateCustomTemplateFromVm triggers async snapshot of the given VM into a new
// custom template.
// POST /v1/custom-templates/from-vm/{vm_id}
func (c *Client) CreateCustomTemplateFromVm(ctx context.Context, vmID string, req CustomTemplateCreateRequest) (*CustomTemplate, error) {
	var out CustomTemplate
	if err := c.do(ctx, http.MethodPost, "/v1/custom-templates/from-vm/"+vmID, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateCustomTemplate updates name / description (only).
// PATCH /v1/custom-templates/{id}
func (c *Client) UpdateCustomTemplate(ctx context.Context, id string, req CustomTemplateUpdateRequest) (*CustomTemplate, error) {
	var out CustomTemplate
	if err := c.do(ctx, http.MethodPatch, "/v1/custom-templates/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCustomTemplate removes a custom template.
// DELETE /v1/custom-templates/{id}
func (c *Client) DeleteCustomTemplate(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/custom-templates/"+id, nil, nil)
}
