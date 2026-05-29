package client

import "time"

// SSHKey represents an SSH key resource (synchronous).
//
// Scope is one of "user" (visible only to its creator, survives org switches),
// "org" (visible inside the currently active organization — admin+/owner
// create) or "tenant" (visible to every org and every invited member of the
// tenant — owner-only create). Defaults server-side to "user" when omitted.
// CreatedByTenantID is the UUID of the tenant the key was created from; the
// field is null on legacy rows predating the scoping migration.
type SSHKey struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Fingerprint       string    `json:"fingerprint"`
	Scope             string    `json:"scope,omitempty"`
	CreatedByTenantID string    `json:"created_by_tenant_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type SSHKeyCreateRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	// Scope is optional — defaults to "user" server-side when empty.
	Scope string `json:"scope,omitempty"`
}

// VPC represents a VPC resource. Status: active | deleting | error.
type VPC struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	VlanID    *int      `json:"vlan_id,omitempty"`
	SDNType   string    `json:"sdn_type"`
	Status    string    `json:"status"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

type VPCCreateRequest struct {
	Name   string   `json:"name"`
	Region string   `json:"region"`
	Tags   []string `json:"tags,omitempty"`
}

// VNet represents a VNet resource (nested under VPC). Status: active | deleting | error.
type VNet struct {
	ID        string    `json:"id"`
	VPCID     string    `json:"vpc_id"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	Gateway   *string   `json:"gateway,omitempty"`
	DHCPStart *string   `json:"dhcp_start,omitempty"`
	DHCPEnd   *string   `json:"dhcp_end,omitempty"`
	SNAT      bool      `json:"snat"`
	Isolated  bool      `json:"isolated"`
	Status    string    `json:"status"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

type VNetCreateRequest struct {
	Name      string   `json:"name"`
	CIDR      string   `json:"cidr"`
	DHCPStart *string  `json:"dhcp_start,omitempty"`
	DHCPEnd   *string  `json:"dhcp_end,omitempty"`
	SNAT      bool     `json:"snat"`
	Tags      []string `json:"tags,omitempty"`
}

// VNetUpdateRequest — only name + snat are mutable per API spec.
type VNetUpdateRequest struct {
	Name *string `json:"name,omitempty"`
	SNAT *bool   `json:"snat,omitempty"`
}

// Region represents a static region entry.
type Region struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Location  string `json:"location"`
	Country   string `json:"country"`
	Flag      string `json:"flag"`
	Available bool   `json:"available"`
}

// Status helpers — VPC/VNet share these values.
const (
	StatusActive   = "active"
	StatusDeleting = "deleting"
	StatusError    = "error"
)

// Container represents an LXC container instance.
// Status: provisioning | running | stopped | error | deleting.
type Container struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Region          string    `json:"region"`
	Plan            string    `json:"plan"`
	Cores           int       `json:"cores"`
	MemoryMB        int       `json:"memory_mb"`
	DiskGB          int       `json:"disk_gb"`
	Template        string    `json:"template"`
	Status          string    `json:"status"`
	IPAddress       *string   `json:"ip_address,omitempty"`
	PublicIPAddress *string   `json:"public_ip_address,omitempty"`
	VnetID          *string   `json:"vnet_id,omitempty"`
	ScaleSetID      *string   `json:"scale_set_id,omitempty"`
	UserData        *string   `json:"user_data,omitempty"`
	ErrorMessage    *string   `json:"error_message,omitempty"`
	HasRootPassword bool      `json:"has_root_password"`
	Tags            []string  `json:"tags"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ContainerCreateRequest struct {
	Name         string   `json:"name"`
	Region       string   `json:"region"`
	Plan         string   `json:"plan"`
	Template     string   `json:"template"`
	VnetID       *string  `json:"vnet_id,omitempty"`
	SSHKeyIDs    []string `json:"ssh_key_ids,omitempty"`
	UserData     *string  `json:"user_data,omitempty"`
	PublicIPID   *string  `json:"public_ip_id,omitempty"`
	RootPassword *string  `json:"root_password,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// ContainerActionRequest is sent to /actions: action ∈ {start, stop, restart}.
type ContainerActionRequest struct {
	Action string `json:"action"`
}

// Container statuses.
const (
	ContainerStatusProvisioning = "provisioning"
	ContainerStatusRunning      = "running"
	ContainerStatusStopped      = "stopped"
	ContainerStatusError        = "error"
	ContainerStatusDeleting     = "deleting"
)

// BlockVolume represents a Ceph RBD volume.
// Status: creating | available | attached | detaching | deleting | error.
type BlockVolume struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Region         string    `json:"region"`
	SizeGB         int       `json:"size_gb"`
	Status         string    `json:"status"`
	AttachedToID   *string   `json:"attached_to_id,omitempty"`
	AttachedToType *string   `json:"attached_to_type,omitempty"`
	AttachedToName *string   `json:"attached_to_name,omitempty"`
	RBDPool        *string   `json:"rbd_pool,omitempty"`
	RBDImage       *string   `json:"rbd_image,omitempty"`
	ErrorMessage   *string   `json:"error_message,omitempty"`
	Tags           []string  `json:"tags"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type BlockVolumeCreateRequest struct {
	Name   string   `json:"name"`
	Region string   `json:"region"`
	SizeGB int      `json:"size_gb"`
	Tags   []string `json:"tags,omitempty"`
}

type BlockVolumeAttachRequest struct {
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"` // "container" | "vm"
}

type BlockVolumeResizeRequest struct {
	SizeGB int `json:"size_gb"`
}

const (
	VolumeStatusCreating  = "creating"
	VolumeStatusAvailable = "available"
	VolumeStatusAttached  = "attached"
	VolumeStatusDetaching = "detaching"
	VolumeStatusDeleting  = "deleting"
	VolumeStatusError     = "error"
)

// PublicIP represents an allocated public IP address.
// Status: available | allocated | attached | reserved.
type PublicIP struct {
	ID               string    `json:"id"`
	PoolID           string    `json:"pool_id"`
	Region           string    `json:"region"`
	IPAddress        string    `json:"ip_address"`
	Status           string    `json:"status"`
	ContainerID      *string   `json:"container_id,omitempty"`
	VMInstanceID     *string   `json:"vm_instance_id,omitempty"`
	LoadBalancerID   *string   `json:"load_balancer_id,omitempty"`
	LoadBalancerName *string   `json:"load_balancer_name,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type PublicIPAllocateRequest struct {
	Region string  `json:"region"`
	PoolID *string `json:"pool_id,omitempty"`
}

type PublicIPAttachRequest struct {
	ResourceType string `json:"resource_type"` // "container" | "vm_instance"
	ResourceID   string `json:"resource_id"`
}

const (
	PublicIPStatusAvailable = "available"
	PublicIPStatusAllocated = "allocated"
	PublicIPStatusAttached  = "attached"
	PublicIPStatusReserved  = "reserved"
)

// ObjectBucket represents a Ceph RGW S3 bucket.
// Status: creating | active | deleting | error.
type ObjectBucket struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Region       string    `json:"region"`
	EndpointURL  *string   `json:"endpoint_url,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	Status       string    `json:"status"`
	IsPublic     bool      `json:"is_public"`
	ErrorMessage *string   `json:"error_message,omitempty"`
	Tags         []string  `json:"tags"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ObjectBucketCreateRequest struct {
	Name     string   `json:"name"`
	Region   string   `json:"region"`
	IsPublic bool     `json:"is_public"`
	Tags     []string `json:"tags,omitempty"`
}

type ObjectBucketUpdateRequest struct {
	IsPublic *bool `json:"is_public,omitempty"`
}

// ObjectBucketCredentials returned by GET /v1/buckets/{id}/credentials.
// Master S3 key is tenant-region-wide (covers all buckets of the tenant in
// the region). Sensitive — never store in Terraform state.
type ObjectBucketCredentials struct {
	BucketID    string `json:"bucket_id"`
	BucketName  string `json:"bucket_name"`
	EndpointURL string `json:"endpoint_url"`
	AccessKey   string `json:"access_key"`
	SecretKey   string `json:"secret_key"`
	Region      string `json:"region"`
}

const (
	BucketStatusCreating = "creating"
	BucketStatusActive   = "active"
	BucketStatusDeleting = "deleting"
	BucketStatusError    = "error"
)

// VMInstance represents a QEMU VM instance.
// Status: provisioning | running | stopped | error | deleting.
type VMInstance struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Region          string    `json:"region"`
	Plan            string    `json:"plan"`
	Cores           int       `json:"cores"`
	MemoryMB        int       `json:"memory_mb"`
	DiskGB          int       `json:"disk_gb"`
	Template        string    `json:"template"`
	Status          string    `json:"status"`
	IPAddress       *string   `json:"ip_address,omitempty"`
	PublicIPAddress *string   `json:"public_ip_address,omitempty"`
	VnetID          *string   `json:"vnet_id,omitempty"`
	ScaleSetID      *string   `json:"scale_set_id,omitempty"`
	UserData        *string   `json:"user_data,omitempty"`
	ErrorMessage    *string   `json:"error_message,omitempty"`
	HasRootPassword bool      `json:"has_root_password"`
	Tags            []string  `json:"tags"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type VMInstanceCreateRequest struct {
	Name         string   `json:"name"`
	Region       string   `json:"region"`
	Plan         string   `json:"plan"`
	Template     string   `json:"template,omitempty"`
	VnetID       *string  `json:"vnet_id,omitempty"`
	SSHKeyIDs    []string `json:"ssh_key_ids,omitempty"`
	UserData     *string  `json:"user_data,omitempty"`
	PublicIPID   *string  `json:"public_ip_id,omitempty"`
	RootPassword *string  `json:"root_password,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// VMInstanceUpdateRequest — only metadata mutable (name + tags).
type VMInstanceUpdateRequest struct {
	Name *string  `json:"name,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

// VMActionRequest — action ∈ {start, stop, shutdown, reboot}.
type VMActionRequest struct {
	Action string `json:"action"`
}

const (
	VMStatusProvisioning = "provisioning"
	VMStatusRunning      = "running"
	VMStatusStopped      = "stopped"
	VMStatusError        = "error"
	VMStatusDeleting     = "deleting"
)

// Organization represents an accessible org for the current auth context.
//
// One API key is bound to one org (`api_keys.org_id`); to target a different
// org, use a different API key (typically via Terraform provider aliases).
type Organization struct {
	ID                     string    `json:"id"`
	OwnerTenantID          string    `json:"owner_tenant_id"`
	Name                   string    `json:"name"`
	Description            *string   `json:"description,omitempty"`
	IsDefault              bool      `json:"is_default"`
	Tags                   []string  `json:"tags"`
	DefaultPaymentMethodID *string   `json:"default_payment_method_id,omitempty"`
	HasPaymentMethod       bool      `json:"has_payment_method"`
	HasSubscription        bool      `json:"has_subscription"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ─── Load Balancer (Phase 4) ─────────────────────────────────────────────────
//
// The TF provider schema deliberately omits listeners + backends — these are
// dynamic and best managed via `ccp_lb_listener` / `ccp_lb_backend` (future
// resources) or directly through the API. The TF state owns the LB lifecycle
// (create + delete + tags + IP attachment) only.

type LoadBalancer struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Region          string       `json:"region"`
	Plan            string       `json:"plan"`
	VnetID          string       `json:"vnet_id"`
	VIPAddress      *string      `json:"vip_address,omitempty"`
	PublicIPAddress *string      `json:"public_ip_address,omitempty"`
	PublicIPID      *string      `json:"public_ip_id,omitempty"`
	Status          string       `json:"status"`
	ErrorMessage    *string      `json:"error_message,omitempty"`
	Tags            []string     `json:"tags"`
	Listeners       []LBListener `json:"listeners"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
}

type LBListener struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Algorithm    string      `json:"algorithm"`
	Protocol     string      `json:"protocol"`
	FrontendPort int         `json:"frontend_port"`
	Backends     []LBBackend `json:"backends"`
}

type LBBackend struct {
	ID          string  `json:"id"`
	ContainerID *string `json:"container_id,omitempty"`
	VMID        *string `json:"vm_instance_id,omitempty"`
	ScaleSetID  *string `json:"scale_set_id,omitempty"`
	Port        int     `json:"port"`
	Weight      int     `json:"weight"`
}

type LBListenerCreateRequest struct {
	Name         string `json:"name"`
	Algorithm    string `json:"algorithm"`
	Protocol     string `json:"protocol"`
	FrontendPort int    `json:"frontend_port"`
}

type LBBackendCreateRequest struct {
	ContainerID *string `json:"container_id,omitempty"`
	VMID        *string `json:"vm_instance_id,omitempty"`
	ScaleSetID  *string `json:"scale_set_id,omitempty"`
	Port        int     `json:"port"`
	Weight      int     `json:"weight,omitempty"`
}

type LoadBalancerCreateRequest struct {
	Name       string   `json:"name"`
	Region     string   `json:"region"`
	Plan       string   `json:"plan,omitempty"`
	VnetID     string   `json:"vnet_id"`
	PublicIPID *string  `json:"public_ip_id,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type LoadBalancerUpdateRequest struct {
	Name *string  `json:"name,omitempty"`
	Tags []string `json:"tags,omitempty"`
}

type LoadBalancerAttachIPRequest struct {
	PublicIPID string `json:"public_ip_id"`
}

const (
	LbStatusProvisioning = "provisioning"
	LbStatusActive       = "active"
	LbStatusUpdating     = "updating"
	LbStatusError        = "error"
	LbStatusDeleting     = "deleting"
)

// ─── Container Scale Set ─────────────────────────────────────────────────────

type ContainerScaleSet struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Region           string    `json:"region"`
	Plan             string    `json:"plan"`
	Template         string    `json:"template"`
	VnetID           *string   `json:"vnet_id,omitempty"`
	MinInstances     int       `json:"min_instances"`
	MaxInstances     int       `json:"max_instances"`
	DesiredInstances int       `json:"desired_instances"`
	AutoRepair       bool      `json:"auto_repair"`
	Status           string    `json:"status"`
	ErrorMessage     *string   `json:"error_message,omitempty"`
	Tags             []string  `json:"tags"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ContainerScaleSetCreateRequest struct {
	Name             string   `json:"name"`
	Region           string   `json:"region"`
	Plan             string   `json:"plan"`
	Template         string   `json:"template,omitempty"`
	VnetID           *string  `json:"vnet_id,omitempty"`
	SSHKeyIDs        []string `json:"ssh_key_ids,omitempty"`
	UserData         *string  `json:"user_data,omitempty"`
	RootPassword     *string  `json:"root_password,omitempty"`
	MinInstances     int      `json:"min_instances"`
	MaxInstances     int      `json:"max_instances"`
	DesiredInstances int      `json:"desired_instances"`
	AutoRepair       bool     `json:"auto_repair"`
	Tags             []string `json:"tags,omitempty"`
}

type ContainerScaleSetUpdateRequest struct {
	Name             *string  `json:"name,omitempty"`
	MinInstances     *int     `json:"min_instances,omitempty"`
	MaxInstances     *int     `json:"max_instances,omitempty"`
	DesiredInstances *int     `json:"desired_instances,omitempty"`
	AutoRepair       *bool    `json:"auto_repair,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

type ContainerScaleSetScaleRequest struct {
	DesiredInstances int `json:"desired_instances"`
}

const (
	ScaleSetStatusActive   = "active"
	ScaleSetStatusScaling  = "scaling"
	ScaleSetStatusError    = "error"
	ScaleSetStatusDeleting = "deleting"
)

// ─── VM Scale Set ────────────────────────────────────────────────────────────

type VMScaleSet struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Region           string    `json:"region"`
	Plan             string    `json:"plan"`
	Template         string    `json:"template"`
	VnetID           *string   `json:"vnet_id,omitempty"`
	MinInstances     int       `json:"min_instances"`
	MaxInstances     int       `json:"max_instances"`
	DesiredInstances int       `json:"desired_instances"`
	AutoRepair       bool      `json:"auto_repair"`
	Status           string    `json:"status"`
	ErrorMessage     *string   `json:"error_message,omitempty"`
	Tags             []string  `json:"tags"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type VMScaleSetCreateRequest struct {
	Name             string   `json:"name"`
	Region           string   `json:"region"`
	Plan             string   `json:"plan"`
	Template         string   `json:"template,omitempty"`
	VnetID           *string  `json:"vnet_id,omitempty"`
	SSHKeyIDs        []string `json:"ssh_key_ids,omitempty"`
	UserData         *string  `json:"user_data,omitempty"`
	RootPassword     *string  `json:"root_password,omitempty"`
	MinInstances     int      `json:"min_instances"`
	MaxInstances     int      `json:"max_instances"`
	DesiredInstances int      `json:"desired_instances"`
	AutoRepair       bool     `json:"auto_repair"`
	Tags             []string `json:"tags,omitempty"`
}

type VMScaleSetUpdateRequest struct {
	Name             *string  `json:"name,omitempty"`
	MinInstances     *int     `json:"min_instances,omitempty"`
	MaxInstances     *int     `json:"max_instances,omitempty"`
	DesiredInstances *int     `json:"desired_instances,omitempty"`
	AutoRepair       *bool    `json:"auto_repair,omitempty"`
	Tags             []string `json:"tags,omitempty"`
}

// ─── K8s Cluster (CLKS — Phase 6) ───────────────────────────────────────────
//
// Cluster Kubernetes tenant managé via CAPI/CAPMOX. Le node pool initial est
// requis par l'API (default 1 worker `small`). Pour ajouter des pools après
// création, utiliser `ccp_k8s_node_pool` (séparée).

type K8sCluster struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	DisplayName     *string `json:"display_name,omitempty"`
	Region          string  `json:"region"`
	K8sVersion      string  `json:"k8s_version"`
	OsTemplateKey   string  `json:"os_template_key"`
	VpcID           string  `json:"vpc_id"`
	VnetID          string  `json:"vnet_id"`
	PodCIDR         string  `json:"pod_cidr"`
	ServiceCIDR     string  `json:"service_cidr"`
	ApiEndpoint     *string `json:"api_endpoint,omitempty"`
	PublicIPID      *string `json:"public_ip_id,omitempty"`
	PublicIPAddress *string `json:"public_ip_address,omitempty"`
	// Cluster Autoscaler timers
	AutoscalerScaleDownDelayAfterAdd string `json:"autoscaler_scale_down_delay_after_add"`
	AutoscalerScaleDownUnneededTime  string `json:"autoscaler_scale_down_unneeded_time"`
	// Ingress controller
	IngressControllerEnabled bool    `json:"ingress_controller_enabled"`
	IngressControllerScope   string  `json:"ingress_controller_scope"`
	IngressControllerClass   string  `json:"ingress_controller_class"`
	IngressPublicIPID        *string `json:"ingress_public_ip_id,omitempty"`
	IngressPublicIPAddress   *string `json:"ingress_public_ip_address,omitempty"`
	IngressInternalIP        *string `json:"ingress_internal_ip,omitempty"`
	// Tier — `dev` (single LXC proxy) or `prod` (HA Keepalived VRRP + VIP).
	// Immutable on the backend; provider exposes it as Optional+Computed+ForceNew.
	Tier               string    `json:"tier"`
	ProxySecondaryVmid *int64    `json:"proxy_secondary_vmid,omitempty"`
	ProxySecondaryNode *string   `json:"proxy_secondary_node,omitempty"`
	ProxyVipVnet       *string   `json:"proxy_vip_vnet,omitempty"`
	Status             string    `json:"status"`
	ErrorMessage       *string   `json:"error_message,omitempty"`
	Tags               []string  `json:"tags"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type K8sInitialPool struct {
	Name     string            `json:"name"`
	Plan     string            `json:"plan"`
	Replicas int               `json:"replicas"`
	Labels   map[string]string `json:"labels,omitempty"`
	Taints   []NodePoolTaint   `json:"taints,omitempty"`
	// Autoscaler — min/max (NULL = pool exclu de l'autoscale). Parité avec les
	// node pools additionnels.
	MinSize *int `json:"min_size,omitempty"`
	MaxSize *int `json:"max_size,omitempty"`
}

type K8sClusterCreateRequest struct {
	Name          string         `json:"name"`
	DisplayName   *string        `json:"display_name,omitempty"`
	Region        string         `json:"region"`
	K8sVersion    string         `json:"k8s_version"`
	OsTemplateKey string         `json:"os_template_key"`
	VpcID         string         `json:"vpc_id"`
	VnetID        string         `json:"vnet_id"`
	PodCIDR       string         `json:"pod_cidr,omitempty"`
	ServiceCIDR   string         `json:"service_cidr,omitempty"`
	InitialPool   K8sInitialPool `json:"initial_pool"`
	Tags          []string       `json:"tags,omitempty"`
	// Autoscaler timers
	AutoscalerScaleDownDelayAfterAdd string `json:"autoscaler_scale_down_delay_after_add,omitempty"`
	AutoscalerScaleDownUnneededTime  string `json:"autoscaler_scale_down_unneeded_time,omitempty"`
	// Ingress controller
	IngressControllerEnabled bool    `json:"ingress_controller_enabled"`
	IngressControllerScope   string  `json:"ingress_controller_scope,omitempty"`
	IngressControllerClass   string  `json:"ingress_controller_class,omitempty"`
	IngressPublicIPID        *string `json:"ingress_public_ip_id,omitempty"`
	IngressInternalIP        *string `json:"ingress_internal_ip,omitempty"`
	// Apiserver IP (optionnel, auto-attaché après provisioning)
	ApiserverPublicIPID *string `json:"apiserver_public_ip_id,omitempty"`
	ApiserverInternalIP *string `json:"apiserver_internal_ip,omitempty"`
	// Tier — `dev` (default, single LXC proxy) or `prod` (HA Keepalived VRRP).
	// Immutable on the backend — changing requires recreate.
	Tier string `json:"tier,omitempty"`
}

type K8sClusterUpdateRequest struct {
	DisplayName                      *string  `json:"display_name,omitempty"`
	Tags                             []string `json:"tags,omitempty"`
	AutoscalerScaleDownDelayAfterAdd *string  `json:"autoscaler_scale_down_delay_after_add,omitempty"`
	AutoscalerScaleDownUnneededTime  *string  `json:"autoscaler_scale_down_unneeded_time,omitempty"`
	IngressControllerEnabled         *bool    `json:"ingress_controller_enabled,omitempty"`
	IngressControllerScope           *string  `json:"ingress_controller_scope,omitempty"`
	IngressControllerClass           *string  `json:"ingress_controller_class,omitempty"`
	IngressPublicIPID                *string  `json:"ingress_public_ip_id,omitempty"`
	IngressInternalIP                *string  `json:"ingress_internal_ip,omitempty"`
}

type K8sUpgradeVersionRequest struct {
	K8sVersion string `json:"k8s_version"`
}

type K8sAttachIPRequest struct {
	PublicIPID string `json:"public_ip_id"`
}

const (
	K8sClusterStatusCreating     = "creating"
	K8sClusterStatusProvisioning = "provisioning"
	K8sClusterStatusActive       = "active"
	K8sClusterStatusUpdating     = "updating"
	K8sClusterStatusError        = "error"
	K8sClusterStatusDeleting     = "deleting"
)

// ─── K8s Node Pool (sub-resource du cluster) ────────────────────────────────

// NodePoolTaint represents a Kubernetes taint applied to all nodes of a pool.
// effect must be one of: NoSchedule, PreferNoSchedule, NoExecute.
type NodePoolTaint struct {
	Key    string  `json:"key"`
	Value  *string `json:"value,omitempty"`
	Effect string  `json:"effect"`
}

type K8sNodePool struct {
	ID                    string            `json:"id"`
	ClusterID             string            `json:"cluster_id"`
	Name                  string            `json:"name"`
	Plan                  string            `json:"plan"`
	Replicas              int               `json:"replicas"`
	Labels                map[string]string `json:"labels"`
	Taints                []NodePoolTaint   `json:"taints"`
	MinSize               *int              `json:"min_size,omitempty"`
	MaxSize               *int              `json:"max_size,omitempty"`
	MachineDeploymentName *string           `json:"machine_deployment_name,omitempty"`
	Status                string            `json:"status"`
	ErrorMessage          *string           `json:"error_message,omitempty"`
	CreatedAt             time.Time         `json:"created_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
}

type K8sNodePoolCreateRequest struct {
	Name     string            `json:"name"`
	Plan     string            `json:"plan"`
	Replicas int               `json:"replicas"`
	Labels   map[string]string `json:"labels,omitempty"`
	Taints   []NodePoolTaint   `json:"taints,omitempty"`
	MinSize  *int              `json:"min_size,omitempty"`
	MaxSize  *int              `json:"max_size,omitempty"`
}

type K8sNodePoolUpdateRequest struct {
	Replicas *int              `json:"replicas,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Taints   []NodePoolTaint   `json:"taints,omitempty"`
	MinSize  *int              `json:"min_size,omitempty"`
	MaxSize  *int              `json:"max_size,omitempty"`
}

// ─── DB PostgreSQL Instance (DBaaS — Phase 5) ────────────────────────────────

type DbPgInstance struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Region          string   `json:"region"`
	Engine          string   `json:"engine"`
	EngineVersion   *string  `json:"engine_version,omitempty"`
	Tier            string   `json:"tier"`
	Plan            string   `json:"plan"`
	VpcID           string   `json:"vpc_id"`
	VnetID          string   `json:"vnet_id"`
	Status          string   `json:"status"`
	EndpointVnetIP  *string  `json:"endpoint_vnet_ip,omitempty"`
	EndpointPort    *int     `json:"endpoint_port,omitempty"`
	AdminUsername   *string  `json:"admin_username,omitempty"`
	AdminDatabase   *string  `json:"admin_database,omitempty"`
	Replicas        int      `json:"replicas"`
	StorageGB       int      `json:"storage_gb"`
	CPUMillicores   int      `json:"cpu_millicores"`
	MemoryMB        int      `json:"memory_mb"`
	ErrorMessage    *string  `json:"error_message,omitempty"`
	Tags            []string `json:"tags"`
	PublicIPID      *string  `json:"public_ip_id,omitempty"`
	PublicIPAddress *string  `json:"public_ip_address,omitempty"`
}

type DbPgInstanceCreateRequest struct {
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	VpcID         string   `json:"vpc_id"`
	VnetID        string   `json:"vnet_id"`
	Replicas      *int     `json:"replicas,omitempty"`
	Tier          *string  `json:"tier,omitempty"`
	Plan          string   `json:"plan"`
	StorageGB     int      `json:"storage_gb"`
	EngineVersion *string  `json:"engine_version,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

type DbPgInstanceUpdateRequest struct {
	Tags []string `json:"tags,omitempty"`
}

// ─── DBaaS — Valkey (Phase 5) ────────────────────────────────────────────────

type DbValkeyInstance struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Region          string   `json:"region"`
	Engine          string   `json:"engine"`
	EngineVersion   *string  `json:"engine_version,omitempty"`
	Tier            string   `json:"tier"`
	Plan            string   `json:"plan"`
	VpcID           string   `json:"vpc_id"`
	VnetID          string   `json:"vnet_id"`
	Status          string   `json:"status"`
	EndpointVnetIP  *string  `json:"endpoint_vnet_ip,omitempty"`
	EndpointPort    *int     `json:"endpoint_port,omitempty"`
	Replicas        int      `json:"replicas"`
	StorageGB       int      `json:"storage_gb"`
	CPUMillicores   int      `json:"cpu_millicores"`
	MemoryMB        int      `json:"memory_mb"`
	ErrorMessage    *string  `json:"error_message,omitempty"`
	Tags            []string `json:"tags"`
	PublicIPID      *string  `json:"public_ip_id,omitempty"`
	PublicIPAddress *string  `json:"public_ip_address,omitempty"`
}

type DbValkeyInstanceCreateRequest struct {
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	VpcID         string   `json:"vpc_id"`
	VnetID        string   `json:"vnet_id"`
	Replicas      *int     `json:"replicas,omitempty"`
	Tier          *string  `json:"tier,omitempty"`
	Plan          string   `json:"plan"`
	StorageGB     int      `json:"storage_gb"`
	EngineVersion *string  `json:"engine_version,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// ─── DBaaS — MariaDB (Phase 5) ───────────────────────────────────────────────

type DbMysqlInstance struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Region          string   `json:"region"`
	Engine          string   `json:"engine"`
	EngineVersion   *string  `json:"engine_version,omitempty"`
	Tier            string   `json:"tier"`
	Plan            string   `json:"plan"`
	VpcID           string   `json:"vpc_id"`
	VnetID          string   `json:"vnet_id"`
	Status          string   `json:"status"`
	EndpointVnetIP  *string  `json:"endpoint_vnet_ip,omitempty"`
	EndpointPort    *int     `json:"endpoint_port,omitempty"`
	AdminUsername   *string  `json:"admin_username,omitempty"`
	AdminDatabase   *string  `json:"admin_database,omitempty"`
	Replicas        int      `json:"replicas"`
	StorageGB       int      `json:"storage_gb"`
	CPUMillicores   int      `json:"cpu_millicores"`
	MemoryMB        int      `json:"memory_mb"`
	ErrorMessage    *string  `json:"error_message,omitempty"`
	Tags            []string `json:"tags"`
	PublicIPID      *string  `json:"public_ip_id,omitempty"`
	PublicIPAddress *string  `json:"public_ip_address,omitempty"`
}

type DbMysqlInstanceCreateRequest struct {
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	VpcID         string   `json:"vpc_id"`
	VnetID        string   `json:"vnet_id"`
	Replicas      *int     `json:"replicas,omitempty"`
	Tier          *string  `json:"tier,omitempty"`
	Plan          string   `json:"plan"`
	StorageGB     int      `json:"storage_gb"`
	EngineVersion *string  `json:"engine_version,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// ─── DBaaS — FerretDB (Phase 5) ──────────────────────────────────────────────

type DbFerretdbInstance struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Region          string   `json:"region"`
	Engine          string   `json:"engine"`
	EngineVersion   *string  `json:"engine_version,omitempty"`
	Tier            string   `json:"tier"`
	Plan            string   `json:"plan"`
	VpcID           string   `json:"vpc_id"`
	VnetID          string   `json:"vnet_id"`
	Status          string   `json:"status"`
	EndpointVnetIP  *string  `json:"endpoint_vnet_ip,omitempty"`
	EndpointPort    *int     `json:"endpoint_port,omitempty"`
	AdminUsername   *string  `json:"admin_username,omitempty"`
	AdminDatabase   *string  `json:"admin_database,omitempty"`
	Replicas        int      `json:"replicas"`
	StorageGB       int      `json:"storage_gb"`
	CPUMillicores   int      `json:"cpu_millicores"`
	MemoryMB        int      `json:"memory_mb"`
	ErrorMessage    *string  `json:"error_message,omitempty"`
	Tags            []string `json:"tags"`
	PublicIPID      *string  `json:"public_ip_id,omitempty"`
	PublicIPAddress *string  `json:"public_ip_address,omitempty"`
}

type DbFerretdbInstanceCreateRequest struct {
	Name          string   `json:"name"`
	Region        string   `json:"region"`
	VpcID         string   `json:"vpc_id"`
	VnetID        string   `json:"vnet_id"`
	Replicas      *int     `json:"replicas,omitempty"`
	Tier          *string  `json:"tier,omitempty"`
	Plan          string   `json:"plan"`
	StorageGB     int      `json:"storage_gb"`
	EngineVersion *string  `json:"engine_version,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// ─── Organization ──────────────────────────────────────────────────────────

type OrganizationResource struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Description      *string   `json:"description,omitempty"`
	IsDefault        bool      `json:"is_default"`
	Tags             []string  `json:"tags"`
	HasPaymentMethod bool      `json:"has_payment_method"`
	HasSubscription  bool      `json:"has_subscription"`
	CreatedAt        time.Time `json:"created_at"`
}

type OrganizationCreateRequest struct {
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type OrganizationUpdateRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ─── API Key ──────────────────────────────────────────────────────────────

type ApiKey struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	Scopes     []string  `json:"scopes"`
	ExpiresAt  *string   `json:"expires_at,omitempty"`
	LastUsedAt *string   `json:"last_used_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ApiKeyCreateResponse struct {
	ApiKey
	Token string `json:"token"` // retourné UNE FOIS au create
}

type ApiKeyCreateRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays *int     `json:"expires_in_days,omitempty"`
}

// ─── Org Member ───────────────────────────────────────────────────────────

type OrgMember struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Role           string    `json:"role"` // owner | admin | member | viewer
	MemberTenantID *string   `json:"member_tenant_id,omitempty"`
	AcceptedAt     *string   `json:"accepted_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type OrgMemberCreateRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type OrgMemberUpdateRequest struct {
	Role string `json:"role"`
}

// ─── VNet Peering ─────────────────────────────────────────────────────────

type VnetPeering struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	VnetAID      string    `json:"vnet_a_id"`
	VnetBID      string    `json:"vnet_b_id"`
	Tags         []string  `json:"tags"`
	Status       string    `json:"status"`
	ErrorMessage *string   `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type VnetPeeringCreateRequest struct {
	Name    string   `json:"name"`
	VnetAID string   `json:"vnet_a_id"`
	VnetBID string   `json:"vnet_b_id"`
	Tags    []string `json:"tags,omitempty"`
}

// ─── VPC Peering ──────────────────────────────────────────────────────────

// ─── Support Ticket ────────────────────────────────────────────────────────

type SupportTicket struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Category  string    `json:"category"` // bug | feature | billing | network | infra | question
	Priority  string    `json:"priority"` // low | normal | high | urgent
	Status    string    `json:"status"`   // open | pending_admin | pending_customer | resolved | closed
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SupportTicketCreateRequest struct {
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Category string `json:"category"`
	Priority string `json:"priority"`
}

// ─── Object Storage Key (subuser RGW) ─────────────────────────────────────

type ObjectStorageKey struct {
	ID              string `json:"id"`
	Region          string `json:"region"`
	Label           string `json:"label"`
	AccessLevel     string `json:"access_level"` // read|write|readwrite|full
	AccessKeyPrefix string `json:"access_key_prefix"`
	// Credentials — only returned at creation time.
	AccessKey   string  `json:"access_key,omitempty"`
	SecretKey   string  `json:"secret_key,omitempty"`
	EndpointURL string  `json:"endpoint_url,omitempty"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
	RevokedAt   *string `json:"revoked_at,omitempty"`
}

type ObjectStorageKeyCreateRequest struct {
	Region        string `json:"region"`
	Label         string `json:"label"`
	AccessLevel   string `json:"access_level"`
	ExpiresInDays *int   `json:"expires_in_days,omitempty"`
}

// ─── IPaaS Pool (admin only) ───────────────────────────────────────────────

type IpaasPool struct {
	ID        string    `json:"id"`
	Region    string    `json:"region"`
	CIDR      string    `json:"cidr"`
	Gateway   string    `json:"gateway"`
	Kind      string    `json:"kind"`
	EdgeID    *string   `json:"edge_id,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type IpaasPoolCreateRequest struct {
	Region   string  `json:"region"`
	CIDR     string  `json:"cidr"`
	Gateway  string  `json:"gateway"`
	EdgeID   *string `json:"edge_id,omitempty"`
	IsActive bool    `json:"is_active"`
}

type IpaasPoolUpdateRequest struct {
	IsActive *bool   `json:"is_active,omitempty"`
	EdgeID   *string `json:"edge_id,omitempty"`
}

// ─── Quota Request ─────────────────────────────────────────────────────────

type QuotaRequest struct {
	ID             string    `json:"id"`
	Field          string    `json:"field"`
	CurrentValue   int       `json:"current_value"`
	RequestedValue int       `json:"requested_value"`
	Reason         *string   `json:"reason,omitempty"`
	Status         string    `json:"status"` // pending | approved | rejected
	AdminNote      *string   `json:"admin_note,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type QuotaRequestCreateRequest struct {
	Field          string  `json:"field"`
	RequestedValue int     `json:"requested_value"`
	Reason         *string `json:"reason,omitempty"`
}

// ─── Container Snapshot ─────────────────────────────────────────────────────

type ContainerSnapshot struct {
	ID          string  `json:"id"`
	ContainerID string  `json:"container_id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	SizeBytes   *int64  `json:"size_bytes,omitempty"`
	ErrorMsg    *string `json:"error_message,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type ContainerSnapshotCreateRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ─── VM Snapshot ─────────────────────────────────────────────────────────────

type VmSnapshot struct {
	ID           string  `json:"id"`
	VmInstanceID string  `json:"vm_instance_id"`
	Name         string  `json:"name"`
	Description  *string `json:"description,omitempty"`
	Status       string  `json:"status"`
	SizeBytes    *int64  `json:"size_bytes,omitempty"`
	ErrorMsg     *string `json:"error_message,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

type VmSnapshotCreateRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ─── VNet IP Reservation ─────────────────────────────────────────────────────

type VnetIpReservation struct {
	ID          string  `json:"id"`
	VnetID      string  `json:"vnet_id"`
	Name        string  `json:"name"`
	IP          string  `json:"ip"`
	RangeEnd    *string `json:"range_end,omitempty"`
	Description *string `json:"description,omitempty"`
	Count       int     `json:"count"`
	Kind        string  `json:"kind"`
	CreatedAt   string  `json:"created_at"`
}

type VnetIpReservationCreateRequest struct {
	Name        string  `json:"name"`
	IP          string  `json:"ip"`
	RangeEnd    *string `json:"range_end,omitempty"`
	Description *string `json:"description,omitempty"`
}

// ─── VNet Firewall Rule ──────────────────────────────────────────────────────

type VnetFirewallRule struct {
	ID         string  `json:"id"`
	VnetID     string  `json:"vnet_id"`
	Position   int     `json:"position"`
	Direction  string  `json:"direction"`
	Action     string  `json:"action"`
	Proto      *string `json:"proto,omitempty"`
	SourceCIDR *string `json:"source_cidr,omitempty"`
	DestCIDR   *string `json:"dest_cidr,omitempty"`
	Dport      *string `json:"dport,omitempty"`
	Comment    *string `json:"comment,omitempty"`
	Enabled    bool    `json:"enabled"`
	CreatedAt  string  `json:"created_at"`
}

type VnetFirewallRuleCreateRequest struct {
	Direction  string  `json:"direction"`
	Action     string  `json:"action"`
	Proto      *string `json:"proto,omitempty"`
	SourceCIDR *string `json:"source_cidr,omitempty"`
	DestCIDR   *string `json:"dest_cidr,omitempty"`
	Dport      *string `json:"dport,omitempty"`
	Comment    *string `json:"comment,omitempty"`
	Enabled    bool    `json:"enabled"`
	Position   int     `json:"position,omitempty"`
}

// LxcTemplate represents an LXC container template (admin-managed catalog).
// GET /v1/templates
type LxcTemplate struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	IsDefault   bool   `json:"is_default"`
}

// QemuTemplate represents a QEMU/VM template (admin-managed catalog).
// GET /v1/qemu-templates
type QemuTemplate struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	IsDefault   bool   `json:"is_default"`
}

// DbPlan represents a database plan (per engine).
// GET /v1/db/plans?engine=<engine>
type DbPlan struct {
	Key           string   `json:"key"`
	Name          *string  `json:"name,omitempty"`
	Engine        string   `json:"engine"`
	CPUMillicores int      `json:"cpu_millicores"`
	MemoryMB      int      `json:"memory_mb"`
	PriceEURMonth *float64 `json:"price_eur_month,omitempty"`
	IsDefault     bool     `json:"is_default"`
}

// DbEngineVersion represents an active engine version exposed to clients.
// GET /v1/db/engine-versions?engine=<engine>
type DbEngineVersion struct {
	Engine    string  `json:"engine"`
	Version   string  `json:"version"`
	Label     *string `json:"label,omitempty"`
	IsDefault bool    `json:"is_default"`
}

// K8sTemplate represents a Kubernetes node OS template (admin-managed catalog).
// GET /v1/k8s/templates
type K8sTemplate struct {
	OsKey       string  `json:"os_key"`
	OsLabel     string  `json:"os_label"`
	DisplayName string  `json:"display_name"`
	K8sVersion  string  `json:"k8s_version"`
	Region      string  `json:"region"`
	VMID        *int    `json:"vmid,omitempty"`
	BuiltAt     *string `json:"built_at,omitempty"`
}

// CustomTemplate represents a tenant-owned reusable template (snapshot from
// a container or VM, usable as base image for new instances).
// GET/POST/PATCH/DELETE /v1/custom-templates
type CustomTemplate struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Description        *string `json:"description,omitempty"`
	TemplateType       string  `json:"template_type"`
	Region             string  `json:"region"`
	Status             string  `json:"status"`
	ErrorMessage       *string `json:"error_message,omitempty"`
	DiskGB             *int    `json:"disk_gb,omitempty"`
	SourceInstanceID   *string `json:"source_instance_id,omitempty"`
	SourceInstanceType *string `json:"source_instance_type,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type CustomTemplateCreateRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type CustomTemplateUpdateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}
