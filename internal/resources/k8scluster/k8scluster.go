// Package k8scluster implements the ccp_k8s_cluster Terraform resource.
//
// CCKS (CETIC Cloud Kubernetes Service) — cluster K8s tenant managé par
// la plateforme. Le pool de workers initial est créé en même temps que le
// cluster (`initial_pool` block, défaut: 1× small worker `default`).
// Pour gérer des pools additionnels, utiliser `ccp_k8s_node_pool`.
//
// Provisioning asynchrone (5-15 min). Create poll jusqu'à status=active.
// Upgrade de version = appel à /upgrade-version : depuis #2143, plan de
// contrôle SEUL (les pools ne suivent plus), attendu jusqu'à la fin de
// l'opération. Plan du plan de contrôle (`plan`, `tier` déprécié) : plan.go ;
// mise à jour automatique (`maintenance`) : maintenance.go ; SSO (`oidc`) :
// oidc.go ; règle « jamais de retour arrière » des versions : ccks.Floor.
package k8scluster

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/ccks"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*k8sResource)(nil)
	_ resource.ResourceWithConfigure   = (*k8sResource)(nil)
	_ resource.ResourceWithImportState = (*k8sResource)(nil)
)

func New() resource.Resource { return &k8sResource{} }

type k8sResource struct {
	client *client.Client
}

type initialPoolModel struct {
	Name       types.String `tfsdk:"name"`
	Plan       types.String `tfsdk:"plan"`
	Replicas   types.Int64  `tfsdk:"replicas"`
	K8sVersion types.String `tfsdk:"k8s_version"`
	DiskGB     types.Int64  `tfsdk:"disk_gb"`
	Labels     types.Map    `tfsdk:"labels"`
	Taints     types.Set    `tfsdk:"taints"`
	MinSize    types.Int64  `tfsdk:"min_size"`
	MaxSize    types.Int64  `tfsdk:"max_size"`
}

// initialPoolTaintModel mirrors the taint nested object for the initial_pool block.
type initialPoolTaintModel struct {
	Key    types.String `tfsdk:"key"`
	Value  types.String `tfsdk:"value"`
	Effect types.String `tfsdk:"effect"`
}

// initialPoolLabels converts the plan's labels map → map[string]string (nil if unset).
func initialPoolLabels(ctx context.Context, ip *initialPoolModel) map[string]string {
	if ip == nil || ip.Labels.IsNull() || ip.Labels.IsUnknown() {
		return nil
	}
	out := map[string]string{}
	ip.Labels.ElementsAs(ctx, &out, false)
	return out
}

// initialPoolTaints converts the plan's taints set → []client.NodePoolTaint (nil if unset).
func initialPoolTaints(ctx context.Context, ip *initialPoolModel) ([]client.NodePoolTaint, error) {
	if ip == nil || ip.Taints.IsNull() || ip.Taints.IsUnknown() {
		return nil, nil
	}
	var models []initialPoolTaintModel
	if diags := ip.Taints.ElementsAs(ctx, &models, false); diags.HasError() {
		return nil, fmt.Errorf("decoding initial_pool taints: %v", diags)
	}
	taints := make([]client.NodePoolTaint, 0, len(models))
	for _, m := range models {
		t := client.NodePoolTaint{Key: m.Key.ValueString(), Effect: m.Effect.ValueString()}
		if !m.Value.IsNull() && !m.Value.IsUnknown() {
			v := m.Value.ValueString()
			t.Value = &v
		}
		taints = append(taints, t)
	}
	return taints, nil
}

type k8sResourceModel struct {
	ID            types.String      `tfsdk:"id"`
	Name          types.String      `tfsdk:"name"`
	DisplayName   types.String      `tfsdk:"display_name"`
	Region        types.String      `tfsdk:"region"`
	K8sVersion    types.String      `tfsdk:"k8s_version"`
	OsTemplateKey types.String      `tfsdk:"os_template_key"`
	OsImage       types.String      `tfsdk:"os_image"`
	VpcID         types.String      `tfsdk:"vpc_id"`
	VnetID        types.String      `tfsdk:"vnet_id"`
	PodCIDR       types.String      `tfsdk:"pod_cidr"`
	ServiceCIDR   types.String      `tfsdk:"service_cidr"`
	InitialPool   *initialPoolModel `tfsdk:"initial_pool"`
	// Autoscaler timers (mutable)
	AutoscalerScaleDownDelayAfterAdd types.String `tfsdk:"autoscaler_scale_down_delay_after_add"`
	AutoscalerScaleDownUnneededTime  types.String `tfsdk:"autoscaler_scale_down_unneeded_time"`
	// Ingress controller (mutable)
	IngressControllerEnabled types.Bool   `tfsdk:"ingress_controller_enabled"`
	IngressControllerScope   types.String `tfsdk:"ingress_controller_scope"`
	IngressControllerClass   types.String `tfsdk:"ingress_controller_class"`
	IngressPublicIPID        types.String `tfsdk:"ingress_public_ip_id"`
	IngressInternalIP        types.String `tfsdk:"ingress_internal_ip"`
	IngressPublicIPAddress   types.String `tfsdk:"ingress_public_ip_address"`
	// Apiserver public IP : `apiserver_public_ip_id` est mutable (attach/détach),
	// relu du backend. `apiserver_internal_ip` reste create-time (ForceNew).
	ApiserverPublicIPID types.String `tfsdk:"apiserver_public_ip_id"`
	ApiserverInternalIP types.String `tfsdk:"apiserver_internal_ip"`
	PublicIPAddress     types.String `tfsdk:"public_ip_address"`
	// Plan of the control plane (#2143) and its deprecated alias tier.
	Plan            types.String `tfsdk:"plan"`
	Tier            types.String `tfsdk:"tier"`
	EtcdVolumeGB    types.Int64  `tfsdk:"etcd_volume_gb"`
	NodeIPBlockSize types.Int64  `tfsdk:"node_ip_block_size"`
	// Actual control-plane version (k8s_version is the desired one, ccks.Floor).
	CurrentK8sVersion types.String `tfsdk:"current_k8s_version"`
	// Automatic updates (maintenance.go) and SSO (oidc.go).
	Maintenance types.Object `tfsdk:"maintenance"`
	OIDC        types.Object `tfsdk:"oidc"`
	// Proxy HA (read-only, only populated for tier=prod)
	ProxyVipVnet types.String `tfsdk:"proxy_vip_vnet"`
	// Computed
	ApiEndpoint types.String `tfsdk:"api_endpoint"`
	Status      types.String `tfsdk:"status"`
	Tags        types.List   `tfsdk:"tags"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *k8sResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_k8s_cluster"
}

func (r *k8sResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud Kubernetes cluster (CCKS) managed by the platform. " +
			"Provisioning is asynchronous (5-15 min). Create blocks until cluster is active. " +
			"Changing `k8s_version` upgrades the control plane only, in place; node pools are " +
			"upgraded through their own `k8s_version`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "DNS-safe slug (lowercase, digits, hyphens, 1-63 chars). Forces replacement.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 63)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (mutable).",
				Optional:            true,
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"k8s_version": schema.StringAttribute{
				MarkdownDescription: "Desired Kubernetes version of the control plane (ex: `v1.34.8`). " +
					"Raising it upgrades the CONTROL PLANE ONLY, in place (one minor at a time), and waits " +
					"for the end of the upgrade: node pools do not follow any more, they are pinned on " +
					"their version and upgraded through `initial_pool.k8s_version` / " +
					"`ccp_k8s_node_pool.k8s_version`. A value below the version the cluster runs " +
					"(automatic patch, console upgrade) is kept as is: the provider never downgrades. " +
					"The actual version is `current_k8s_version`.",
				Required: true,
			},
			"current_k8s_version": schema.StringAttribute{
				MarkdownDescription: "Kubernetes version the control plane actually runs, as reported by the platform " +
					"(may be above `k8s_version` after an automatic patch).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stateUnlessVersionChanges{}},
			},
			"os_template_key": schema.StringAttribute{
				MarkdownDescription: "VM template key for worker nodes (ex: `clks-capi-debian-13`). Forces replacement. " +
					"The platform rewrites it on a version upgrade; the provider keeps the configured value.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"os_image": schema.StringAttribute{
				MarkdownDescription: "Node operating-system family for the cluster nodes. One of `flatcar`, `ubuntu`, `rocky9`. " +
					"Defaults to `flatcar` when omitted. Forces replacement (changing the node OS recreates the cluster).",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("flatcar", "ubuntu", "rocky9"),
				},
				// UseStateForUnknown FIRST: an unset os_image is unknown in the
				// plan of any other change (Computed), and RequiresReplace would
				// then replace the cluster for a tags change.
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vpc_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"pod_cidr": schema.StringAttribute{
				MarkdownDescription: "Pod CIDR (default `10.244.0.0/16`). Forces replacement.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("10.244.0.0/16"),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"service_cidr": schema.StringAttribute{
				MarkdownDescription: "Service CIDR (default `10.96.0.0/12`). Forces replacement.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("10.96.0.0/12"),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			// ── Autoscaler timers ──────────────────────────────────────────
			"autoscaler_scale_down_delay_after_add": schema.StringAttribute{
				MarkdownDescription: "Cluster Autoscaler — délai avant scale-down après un scale-up (ex: `10m`, `2h`). Mutable.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("10m"),
			},
			"autoscaler_scale_down_unneeded_time": schema.StringAttribute{
				MarkdownDescription: "Cluster Autoscaler — temps d'attente avant suppression d'un nœud inutilisé. Mutable.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("10m"),
			},
			// ── Ingress controller ─────────────────────────────────────────
			"ingress_controller_enabled": schema.BoolAttribute{
				MarkdownDescription: "Déployer un ingress controller dans le cluster. Mutable.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"ingress_controller_scope": schema.StringAttribute{
				MarkdownDescription: "`internal` (VNet seul) ou `external` (IP publique). Mutable.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("internal"),
				Validators: []validator.String{
					stringvalidator.OneOf("internal", "external"),
				},
			},
			"ingress_controller_class": schema.StringAttribute{
				MarkdownDescription: "Implémentation de l'ingress : `incluster` (ingress controller Cilium déployé dans le cluster, IP gérée en interne — aucun load balancer additionnel) ou `managed` (load balancer dédié CETIC Cloud, paire HA avec bascule automatique). Mutable.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("incluster"),
				Validators: []validator.String{
					stringvalidator.OneOf("incluster", "managed"),
				},
			},
			"ingress_public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID d'une IP publique pré-réservée pour l'ingress (scope `external`). Mutable.",
				Optional:            true,
				Computed:            true,
			},
			"ingress_internal_ip": schema.StringAttribute{
				MarkdownDescription: "IP privée VNet pré-réservée pour l'ingress (scope `internal`). Mutable.",
				Optional:            true,
				Computed:            true,
			},
			"ingress_public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Adresse IP publique effective de l'ingress (computed).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			// ── Apiserver public IP (attach/détach, mutable) ───────────────
			"apiserver_public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID d'une IP publique attachée à l'apiserver (kubeconfig public). " +
					"Mutable : définir l'UUID attache l'IP, le retirer (`null`) la détache — " +
					"sans recréer le cluster. Fonctionne à la création comme après coup.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"apiserver_internal_ip": schema.StringAttribute{
				MarkdownDescription: "IP privée VNet pour l'endpoint apiserver. Forces replacement.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Adresse IP publique de l'apiserver (computed).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			// ── Plan of the control plane (#2143) — plan.go ────────────────
			"plan": schema.StringAttribute{
				MarkdownDescription: "Plan of the control plane: `starter` (free, single apiserver frontend, no HA, " +
					"up to 10 nodes), `standard` (HA frontend pair, 2 control-plane replicas, up to 50 nodes) or " +
					"`pro` (3 replicas, up to 150 nodes). Defaults to the plan matching `tier` when only `tier` " +
					"is set, `starter` otherwise. `standard` ↔ `pro` is changed in place; `starter` ↔ a paid " +
					"plan forces replacement (the apiserver frontend changes topology).",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf(planStarter, planStandard, planPro),
				},
				PlanModifiers: []planmodifier.String{planAttrModifier{}},
			},
			"tier": schema.StringAttribute{
				MarkdownDescription: "**Deprecated** — use `plan`. `dev` is `plan = \"starter\"`, `prod` is a paid " +
					"plan (`standard` unless the cluster is already `pro`). Always consistent with `plan`; " +
					"setting both with different topologies is an error.",
				DeprecationMessage: "Use `plan` (starter, standard or pro) instead: `tier = \"dev\"` is " +
					"`plan = \"starter\"`, `tier = \"prod\"` is `plan = \"standard\"`.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf(tierDev, tierProd),
					tierMatchesPlan{},
				},
				PlanModifiers: []planmodifier.String{tierAttrModifier{}},
			},
			"etcd_volume_gb": schema.Int64Attribute{
				MarkdownDescription: "Effective etcd volume (GiB): the plan's at creation, raised on an upgrade of the plan, never reduced.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{stateUnlessPlanChanges{}},
			},
			"node_ip_block_size": schema.Int64Attribute{
				MarkdownDescription: "Size of the node IP block allocated in the VNet (`null` until allocated).",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{stateUnlessPlanChanges{}},
			},
			"maintenance": maintenanceSchema(),
			"oidc":        oidcSchema(),
			"proxy_vip_vnet": schema.StringAttribute{
				MarkdownDescription: "Adresse flottante sur le VNet partagée entre les frontaux apiserver primary+secondary (plans `standard` et `pro`, sinon null). C'est l'IP que le kubeconfig pointe en mode HA.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			// ── Computed ───────────────────────────────────────────────────
			"api_endpoint": schema.StringAttribute{
				MarkdownDescription: "Kubernetes API endpoint (host:port). Available once active.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				// PAS de UseStateForUnknown : `status` est volatil (un update peut
				// le faire passer "active" → "updating") → pinner l'ancien état
				// causerait "Provider produced inconsistent result". Known-after-apply.
				Computed: true,
			},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"initial_pool": schema.SingleNestedBlock{
				MarkdownDescription: "Initial worker pool created with the cluster (default: name=`default`, plan=`small`, replicas=1). `name`/`plan` force replacement ; `replicas`/`min_size`/`max_size`/`labels`/`taints` are mutable in-place.",
				Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{
						Required:      true,
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"plan": schema.StringAttribute{
						Required:      true,
						PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
					},
					"replicas": schema.Int64Attribute{
						Required: true,
					},
					"k8s_version": schema.StringAttribute{
						// Optional (non-Computed) to match the rest of the initial_pool
						// block: stateFromAPI preserves the plan, there is no readback,
						// so the state equals the config → no perma-diff, even when
						// the platform pins the pool on its version after a control
						// plane upgrade (#2143) or patches it automatically.
						MarkdownDescription: "Desired Kubernetes version of the worker nodes in the initial pool " +
							"(`vX.Y.Z`). Omit to keep the version the pool was created with (the control-plane " +
							"version). Raising it upgrades the pool in place through the platform's pool upgrade " +
							"(one node at a time) and waits for the end: the target must be `<=` the control-plane " +
							"version and of the same minor. Since the control-plane upgrade no longer moves the " +
							"pools, raise it after (or in the same apply as) `k8s_version`. A value below the " +
							"version the pool runs is not applied (no downgrade).",
						Optional: true,
					},
					"disk_gb": schema.Int64Attribute{
						MarkdownDescription: "Root disk size (GB) of every node in the initial pool. " +
							"Optional — the platform picks 20 GB (or the plan's minimum disk if higher) when omitted. No resize " +
							"endpoint exists for node pools, so changing it forces destroy + recreate " +
							"of the cluster (like `name`/`plan`).",
						Optional: true,
						Validators: []validator.Int64{
							int64validator.AtLeast(1),
						},
						PlanModifiers: []planmodifier.Int64{
							int64planmodifier.RequiresReplace(),
						},
					},
					"labels": schema.MapAttribute{
						MarkdownDescription: "Kubernetes labels propagated to the initial pool's nodes (parity with `ccp_k8s_node_pool.labels`). Mutable in-place.",
						ElementType:         types.StringType,
						Optional:            true,
					},
					"taints": schema.SetNestedAttribute{
						MarkdownDescription: "Kubernetes taints applied to the initial pool's nodes (parity with `ccp_k8s_node_pool.taints`). Mutable in-place.",
						Optional:            true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"key": schema.StringAttribute{
									Required:            true,
									MarkdownDescription: "Taint key.",
								},
								"value": schema.StringAttribute{
									Optional:            true,
									MarkdownDescription: "Taint value (may be empty).",
								},
								"effect": schema.StringAttribute{
									Required:            true,
									MarkdownDescription: "Taint effect. One of: `NoSchedule`, `PreferNoSchedule`, `NoExecute`.",
									Validators: []validator.String{
										stringvalidator.OneOf("NoSchedule", "PreferNoSchedule", "NoExecute"),
									},
								},
							},
						},
					},
					"min_size": schema.Int64Attribute{
						MarkdownDescription: "Cluster autoscaler lower bound for this pool. Set `min_size` **and** `max_size` to enable the autoscaler on the initial pool (mutable in-place). Leave both unset for a fixed-size pool.",
						Optional:            true,
					},
					"max_size": schema.Int64Attribute{
						MarkdownDescription: "Cluster autoscaler upper bound for this pool. Requires `min_size`.",
						Optional:            true,
					},
				},
			},
		},
	}
}

func (r *k8sResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = c
}

// stateFromAPI builds the state from an API response. prior (the plan on
// Create/Update, the state on Read, nil on import) supplies what the API does
// not return or rewrites:
//   - `initial_pool` and `apiserver_internal_ip`, never read back;
//   - `k8s_version`, the desired version (ccks.Floor: kept while the cluster
//     runs it or higher);
//   - `os_template_key`, rewritten by the platform on every version upgrade
//     (`kube-vX-Y-Z`) — read back, it would force a replacement;
//   - `oidc`, kept in the configured spelling when it is the same
//     configuration;
//   - `maintenance`, copied here and set by the caller from its own route.
func stateFromAPI(ctx context.Context, c *client.K8sCluster, prior *k8sResourceModel) (k8sResourceModel, []string) {
	if prior == nil {
		prior = &k8sResourceModel{}
	}
	plan := planOrTier(c.Plan, c.Tier)
	m := k8sResourceModel{
		ID:            types.StringValue(c.ID),
		Name:          types.StringValue(c.Name),
		Region:        types.StringValue(c.Region),
		K8sVersion:    ccks.Floor(prior.K8sVersion, &c.K8sVersion),
		OsTemplateKey: types.StringValue(c.OsTemplateKey),
		OsImage:       types.StringValue(c.OsImage),
		VpcID:         types.StringValue(c.VpcID),
		VnetID:        types.StringValue(c.VnetID),
		PodCIDR:       types.StringValue(c.PodCIDR),
		ServiceCIDR:   types.StringValue(c.ServiceCIDR),
		Status:        types.StringValue(c.Status),
		CreatedAt:     types.StringValue(c.CreatedAt.Format("2006-01-02T15:04:05Z07:00")),
		InitialPool:   prior.InitialPool,
		// Autoscaler
		AutoscalerScaleDownDelayAfterAdd: types.StringValue(c.AutoscalerScaleDownDelayAfterAdd),
		AutoscalerScaleDownUnneededTime:  types.StringValue(c.AutoscalerScaleDownUnneededTime),
		// Ingress
		IngressControllerEnabled: types.BoolValue(c.IngressControllerEnabled),
		IngressControllerScope:   types.StringValue(c.IngressControllerScope),
		IngressControllerClass:   types.StringValue(c.IngressControllerClass),
		// Plan — the tier is always the plan's (a legacy row without either is
		// a starter/dev cluster).
		Plan:                types.StringValue(plan),
		Tier:                types.StringValue(tierOf(plan)),
		EtcdVolumeGB:        int64OrNull(c.EtcdVolumeGB),
		NodeIPBlockSize:     int64OrNull(c.NodeIPBlockSize),
		CurrentK8sVersion:   types.StringValue(c.K8sVersion),
		Maintenance:         prior.Maintenance,
		ApiserverInternalIP: prior.ApiserverInternalIP,
	}
	if set(prior.OsTemplateKey) {
		m.OsTemplateKey = prior.OsTemplateKey
	}
	if m.Maintenance.IsUnknown() || m.Maintenance.IsNull() {
		m.Maintenance = types.ObjectNull(maintenanceAttrTypes)
	}
	var diagStrs []string
	oidc, d := oidcValue(ctx, c.OIDC, prior.OIDC)
	for _, e := range d.Errors() {
		diagStrs = append(diagStrs, e.Summary()+": "+e.Detail())
	}
	m.OIDC = oidc

	// Proxy HA fields are only populated for a paid plan; nullable on starter.
	m.ProxyVipVnet = stringOrNull(c.ProxyVipVnet)
	m.DisplayName = stringOrNull(c.DisplayName)
	m.ApiEndpoint = stringOrNull(c.ApiEndpoint)
	// Le backend expose l'IP publique apiserver via `public_ip_id` ; côté
	// provider c'est l'unique attribut `apiserver_public_ip_id` (mutable).
	m.ApiserverPublicIPID = stringOrNull(c.PublicIPID)
	m.PublicIPAddress = stringOrNull(c.PublicIPAddress)
	m.IngressPublicIPID = stringOrNull(c.IngressPublicIPID)
	m.IngressPublicIPAddress = stringOrNull(c.IngressPublicIPAddress)
	m.IngressInternalIP = stringOrNull(c.IngressInternalIP)

	tags, diag := types.ListValueFrom(ctx, types.StringType, c.Tags)
	if diag.HasError() {
		for _, d := range diag.Errors() {
			diagStrs = append(diagStrs, d.Summary()+": "+d.Detail())
		}
	}
	m.Tags = tags
	return m, diagStrs
}

func stringOrNull(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

func int64OrNull(p *int) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*p))
}

func warnDiags(resp *diag.Diagnostics, ds []string) {
	for _, d := range ds {
		resp.AddWarning("State conversion warning", d)
	}
}

// readMaintenance sets m.Maintenance from the API. A 404 (API older than
// #2143) keeps the prior value rather than failing the whole read.
func (r *k8sResource) readMaintenance(ctx context.Context, id string, m *k8sResourceModel, prior types.Object) diag.Diagnostics {
	var diags diag.Diagnostics
	got, err := r.client.GetK8sClusterMaintenance(ctx, id)
	if err != nil {
		if client.IsNotFound(err) {
			if prior.IsUnknown() {
				prior = types.ObjectNull(maintenanceAttrTypes)
			}
			m.Maintenance = prior
			return diags
		}
		diags.AddError("Failed to read the maintenance settings of the K8s cluster", err.Error())
		return diags
	}
	v, d := maintenanceValue(ctx, got, prior)
	diags.Append(d...)
	m.Maintenance = v
	return diags
}

// applyMaintenance sends the configured maintenance fields that differ from
// prior (nothing configured or nothing changed: a plain read) and sets
// m.Maintenance from the API answer.
func (r *k8sResource) applyMaintenance(ctx context.Context, id string, m *k8sResourceModel, cfg, prior, planned types.Object) diag.Diagnostics {
	req, send, diags := maintenanceRequest(ctx, cfg, prior)
	if diags.HasError() {
		return diags
	}
	if !send {
		diags.Append(r.readMaintenance(ctx, id, m, planned)...)
		return diags
	}
	got, err := r.client.UpdateK8sClusterMaintenance(ctx, id, req)
	if err != nil {
		diags.AddError("Failed to update the maintenance settings of the K8s cluster", err.Error())
		return diags
	}
	v, d := maintenanceValue(ctx, got, planned)
	diags.Append(d...)
	m.Maintenance = v
	return diags
}

// applyOIDC reconciles the SSO: DELETE when removed, PUT when the
// configuration differs from prior once canonical — never otherwise (a PUT
// of a new configuration restarts the apiserver). Returns the cluster read
// after the call, nil when nothing was sent.
func (r *k8sResource) applyOIDC(ctx context.Context, id string, planned, prior types.Object) (*client.K8sCluster, diag.Diagnostics) {
	var diags diag.Diagnostics
	if planned.IsUnknown() || oidcObjectsSame(ctx, planned, prior) {
		return nil, diags
	}
	if planned.IsNull() {
		got, err := r.client.DeleteK8sClusterOIDC(ctx, id)
		if err != nil {
			diags.AddError("Failed to remove the SSO OIDC of the K8s cluster", err.Error())
		}
		return got, diags
	}
	body, d := oidcFromObject(ctx, planned)
	diags.Append(d...)
	if diags.HasError() {
		return nil, diags
	}
	got, err := r.client.PutK8sClusterOIDC(ctx, id, body)
	if err != nil {
		diags.AddError("Failed to configure the SSO OIDC of the K8s cluster", err.Error())
	}
	return got, diags
}

func (r *k8sResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config k8sResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// initial_pool — defaults si vide
	pool := client.K8sInitialPool{Name: "default", Plan: "small", Replicas: 1}
	if plan.InitialPool != nil {
		pool.Name = plan.InitialPool.Name.ValueString()
		pool.Plan = plan.InitialPool.Plan.ValueString()
		pool.Replicas = int(plan.InitialPool.Replicas.ValueInt64())
		pool.Labels = initialPoolLabels(ctx, plan.InitialPool)
		taints, err := initialPoolTaints(ctx, plan.InitialPool)
		if err != nil {
			resp.Diagnostics.AddError("Invalid initial_pool taints", err.Error())
			return
		}
		pool.Taints = taints
		if !plan.InitialPool.K8sVersion.IsNull() && !plan.InitialPool.K8sVersion.IsUnknown() {
			v := plan.InitialPool.K8sVersion.ValueString()
			pool.K8sVersion = &v
		}
		if !plan.InitialPool.DiskGB.IsNull() && !plan.InitialPool.DiskGB.IsUnknown() {
			v := int(plan.InitialPool.DiskGB.ValueInt64())
			pool.DiskGB = &v
		}
		if !plan.InitialPool.MinSize.IsNull() && !plan.InitialPool.MinSize.IsUnknown() {
			v := int(plan.InitialPool.MinSize.ValueInt64())
			pool.MinSize = &v
		}
		if !plan.InitialPool.MaxSize.IsNull() && !plan.InitialPool.MaxSize.IsUnknown() {
			v := int(plan.InitialPool.MaxSize.ValueInt64())
			pool.MaxSize = &v
		}
	}

	// `plan` is resolved at plan time (plan.go). The deprecated `tier` is sent
	// next to it, always its equivalent: the API accepts both when they agree,
	// and an API older than #2143 still gets the right frontend topology.
	cpPlan, cpTier := "", ""
	if set(plan.Plan) {
		cpPlan = plan.Plan.ValueString()
		cpTier = tierOf(cpPlan)
	}

	// `os_image` is Optional+Computed (server defaults to "flatcar"). Forward it
	// only when the plan resolved to a concrete value; otherwise omit and let
	// the API apply its default (read back into the Computed state).
	osImage := ""
	if !plan.OsImage.IsNull() && !plan.OsImage.IsUnknown() {
		osImage = plan.OsImage.ValueString()
	}

	createReq := client.K8sClusterCreateRequest{
		Name:          plan.Name.ValueString(),
		Region:        plan.Region.ValueString(),
		K8sVersion:    plan.K8sVersion.ValueString(),
		OsTemplateKey: plan.OsTemplateKey.ValueString(),
		OsImage:       osImage,
		VpcID:         plan.VpcID.ValueString(),
		VnetID:        plan.VnetID.ValueString(),
		PodCIDR:       plan.PodCIDR.ValueString(),
		ServiceCIDR:   plan.ServiceCIDR.ValueString(),
		InitialPool:   pool,
		// Autoscaler timers
		AutoscalerScaleDownDelayAfterAdd: plan.AutoscalerScaleDownDelayAfterAdd.ValueString(),
		AutoscalerScaleDownUnneededTime:  plan.AutoscalerScaleDownUnneededTime.ValueString(),
		// Ingress
		IngressControllerEnabled: plan.IngressControllerEnabled.ValueBool(),
		IngressControllerScope:   plan.IngressControllerScope.ValueString(),
		IngressControllerClass:   plan.IngressControllerClass.ValueString(),
		Plan:                     cpPlan,
		Tier:                     cpTier,
	}
	if !plan.DisplayName.IsNull() && !plan.DisplayName.IsUnknown() {
		v := plan.DisplayName.ValueString()
		createReq.DisplayName = &v
	}
	if !plan.IngressPublicIPID.IsNull() && !plan.IngressPublicIPID.IsUnknown() {
		v := plan.IngressPublicIPID.ValueString()
		createReq.IngressPublicIPID = &v
	}
	if !plan.IngressInternalIP.IsNull() && !plan.IngressInternalIP.IsUnknown() {
		v := plan.IngressInternalIP.ValueString()
		createReq.IngressInternalIP = &v
	}
	// `apiserver_public_ip_id` n'est PAS envoyé dans le POST create : il est
	// attaché après provisioning via /attach-ip (cf. plus bas), de façon
	// uniforme avec l'Update et fiable (le provisioning backend ne garantit
	// pas l'attach auto à la création).
	if !plan.ApiserverInternalIP.IsNull() && !plan.ApiserverInternalIP.IsUnknown() {
		v := plan.ApiserverInternalIP.ValueString()
		createReq.ApiserverInternalIP = &v
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags := []string{}
		plan.Tags.ElementsAs(ctx, &tags, false)
		createReq.Tags = tags
	}

	created, err := r.client.CreateK8sCluster(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create K8s cluster", err.Error())
		return
	}

	// The remote object exists from here on. Persist it before waiting: if
	// provisioning fails, Terraform keeps the resource (tainted) and replaces it
	// on the next apply. Dropped from state it would be neither destroyed nor
	// re-planned, and the next apply would strand it and create a second one.
	// Nothing configured after provisioning (SSO, maintenance) is in it yet.
	earlyPrior := plan
	earlyPrior.OIDC = types.ObjectNull(oidcAttrTypes)
	earlyPrior.Maintenance = types.ObjectNull(maintenanceAttrTypes)
	early, _ := stateFromAPI(ctx, created, &earlyPrior)
	resp.Diagnostics.Append(resp.State.Set(ctx, &early)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Poll until active (15 min)
	final, err := pollUntilReady(ctx, r.client, created.ID, 15*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("K8s cluster provisioning timed out or failed", err.Error())
		return
	}

	// Attache l'IP publique apiserver (si fournie) une fois le cluster prêt,
	// via /attach-ip — même chemin que l'Update.
	if !plan.ApiserverPublicIPID.IsNull() && !plan.ApiserverPublicIPID.IsUnknown() &&
		plan.ApiserverPublicIPID.ValueString() != "" {
		if _, err := r.client.AttachIPToK8sCluster(ctx, final.ID,
			client.K8sAttachIPRequest{PublicIPID: plan.ApiserverPublicIPID.ValueString()}); err != nil {
			resp.Diagnostics.AddError("Failed to attach apiserver public IP", err.Error())
			return
		}
		if final, err = r.client.GetK8sCluster(ctx, final.ID); err != nil {
			resp.Diagnostics.AddError("Failed to re-read K8s cluster after IP attach", err.Error())
			return
		}
	}

	// SSO OIDC (needs an active cluster).
	if got, d := r.applyOIDC(ctx, final.ID, plan.OIDC, types.ObjectNull(oidcAttrTypes)); d.HasError() {
		resp.Diagnostics.Append(d...)
		return
	} else if got != nil {
		final = got
	}

	state, diags := stateFromAPI(ctx, final, &plan)
	warnDiags(&resp.Diagnostics, diags)
	resp.Diagnostics.Append(r.applyMaintenance(ctx, final.ID, &state, config.Maintenance,
		types.ObjectNull(maintenanceAttrTypes), plan.Maintenance)...)
	if resp.Diagnostics.HasError() {
		// The cluster exists: keep it in the state without the settings.
		state.Maintenance = types.ObjectNull(maintenanceAttrTypes)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *k8sResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state k8sResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetK8sCluster(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read K8s cluster", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, got, &state)
	warnDiags(&resp.Diagnostics, diags)
	resp.Diagnostics.Append(r.readMaintenance(ctx, got.ID, &newState, state.Maintenance)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

// Timeouts of the control-plane upgrade: waiting for a running upgrade to
// end before starting one, then the upgrade itself (HostedControlPlane
// rollout).
const (
	cpIdleTimeout    = 30 * time.Minute
	cpUpgradeTimeout = 45 * time.Minute
	poolUpgradeTime  = 90 * time.Minute
)

func (r *k8sResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config k8sResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	// ── Control-plane version (#2143: control plane ONLY) ─────────────────
	if !plan.K8sVersion.Equal(state.K8sVersion) {
		target := plan.K8sVersion.ValueString()
		cur, err := r.client.GetK8sCluster(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read K8s cluster before upgrade", err.Error())
			return
		}
		if ccks.IsAbove(target, cur.K8sVersion) {
			if _, err := ccks.WaitClusterIdle(ctx, r.client, id, cpIdleTimeout); err != nil {
				resp.Diagnostics.AddError("K8s cluster not ready for an upgrade", err.Error())
				return
			}
			upgraded, err := r.client.UpgradeK8sClusterVersion(ctx, id,
				client.K8sUpgradeVersionRequest{K8sVersion: ccks.Normalize(target)})
			if err != nil {
				resp.Diagnostics.AddError("Failed to upgrade K8s cluster version", err.Error())
				return
			}
			opID := ""
			if upgraded.UpgradeEnCours != nil {
				opID = upgraded.UpgradeEnCours.ID
			}
			final, err := ccks.WaitUpgradeDone(ctx, r.client, id, opID, cpUpgradeTimeout)
			if err != nil {
				resp.Diagnostics.AddError("K8s cluster upgrade timed out or failed", err.Error())
				return
			}
			// Persist the upgrade before the other changes: a later failure
			// must not plan it again.
			prior := state
			prior.K8sVersion = plan.K8sVersion
			mid, diags := stateFromAPI(ctx, final, &prior)
			warnDiags(&resp.Diagnostics, diags)
			resp.Diagnostics.Append(resp.State.Set(ctx, &mid)...)
			if resp.Diagnostics.HasError() {
				return
			}
		} else if c, ok := ccks.Compare(target, cur.K8sVersion); ok && c < 0 {
			resp.Diagnostics.AddWarning("K8s cluster not downgraded",
				fmt.Sprintf("The control plane runs Kubernetes %s, above the configured k8s_version %s. "+
					"The provider never downgrades: the configured value is recorded, the cluster is left as is.",
					cur.K8sVersion, target))
		}
	}

	// ── Apiserver public IP attach/detach (apiserver_public_ip_id, mutable) ──
	// Skip si la valeur planifiée est encore Unknown (known-after-apply) pour
	// ne pas détacher par erreur sur un "".
	planIPID := plan.ApiserverPublicIPID.ValueString()
	stateIPID := state.ApiserverPublicIPID.ValueString()
	if !plan.ApiserverPublicIPID.IsUnknown() && planIPID != stateIPID {
		if planIPID != "" {
			_, err := r.client.AttachIPToK8sCluster(ctx, id, client.K8sAttachIPRequest{PublicIPID: planIPID})
			if err != nil {
				resp.Diagnostics.AddError("Failed to attach IP to K8s cluster", err.Error())
				return
			}
		} else {
			_, err := r.client.DetachIPFromK8sCluster(ctx, id)
			if err != nil {
				resp.Diagnostics.AddError("Failed to detach IP from K8s cluster", err.Error())
				return
			}
		}
	}

	// ── SSO OIDC — sent only when it changes (apiserver restart) ──────────
	if _, d := r.applyOIDC(ctx, id, plan.OIDC, state.OIDC); d.HasError() {
		resp.Diagnostics.Append(d...)
		return
	}

	// ── Initial pool — réconciliation in-place ────────────────────────────
	// Le backend ne met pas à jour l'initial pool via le PATCH cluster ; il vit
	// comme un node pool. On retrouve ce node pool par son nom : PATCH pour
	// replicas / autoscaler / labels / taints, route d'upgrade de pool (#2143)
	// pour la version.
	if plan.InitialPool != nil && state.InitialPool != nil {
		resp.Diagnostics.Append(r.reconcileInitialPool(ctx, id, plan.InitialPool, state.InitialPool)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// ── Metadata + autoscaler + ingress + plan ─────────────────────────────
	var upd client.K8sClusterUpdateRequest
	// display_name / tags are Optional+Computed: unset, they are unknown in
	// the plan of any other change — sending them then would rename the
	// cluster to "" (display_name) on every update.
	if !plan.DisplayName.Equal(state.DisplayName) && !plan.DisplayName.IsUnknown() {
		v := plan.DisplayName.ValueString()
		upd.DisplayName = &v
	}
	if !plan.Tags.Equal(state.Tags) && !plan.Tags.IsUnknown() {
		tags := []string{}
		if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
			plan.Tags.ElementsAs(ctx, &tags, false)
		}
		upd.Tags = tags
	}
	if !plan.AutoscalerScaleDownDelayAfterAdd.Equal(state.AutoscalerScaleDownDelayAfterAdd) {
		v := plan.AutoscalerScaleDownDelayAfterAdd.ValueString()
		upd.AutoscalerScaleDownDelayAfterAdd = &v
	}
	if !plan.AutoscalerScaleDownUnneededTime.Equal(state.AutoscalerScaleDownUnneededTime) {
		v := plan.AutoscalerScaleDownUnneededTime.ValueString()
		upd.AutoscalerScaleDownUnneededTime = &v
	}
	if !plan.IngressControllerEnabled.Equal(state.IngressControllerEnabled) {
		v := plan.IngressControllerEnabled.ValueBool()
		upd.IngressControllerEnabled = &v
	}
	if !plan.IngressControllerScope.Equal(state.IngressControllerScope) {
		v := plan.IngressControllerScope.ValueString()
		upd.IngressControllerScope = &v
	}
	if !plan.IngressControllerClass.Equal(state.IngressControllerClass) {
		v := plan.IngressControllerClass.ValueString()
		upd.IngressControllerClass = &v
	}
	// Ne jamais envoyer une valeur Computed encore Unknown (known-after-apply)
	// ni une chaîne vide : `ingress_public_ip_id` part alors en `""` → le
	// backend tente de la parser en UUID → 422. Ces champs sont recalculés par
	// le backend selon le scope ingress ; on ne les pousse que si l'utilisateur
	// a fourni une valeur concrète non vide.
	if !plan.IngressPublicIPID.Equal(state.IngressPublicIPID) &&
		!plan.IngressPublicIPID.IsNull() && !plan.IngressPublicIPID.IsUnknown() {
		if v := plan.IngressPublicIPID.ValueString(); v != "" {
			upd.IngressPublicIPID = &v
		}
	}
	if !plan.IngressInternalIP.Equal(state.IngressInternalIP) &&
		!plan.IngressInternalIP.IsNull() && !plan.IngressInternalIP.IsUnknown() {
		if v := plan.IngressInternalIP.ValueString(); v != "" {
			upd.IngressInternalIP = &v
		}
	}
	// Plan of the control plane: Standard ↔ Pro in place (Starter ↔ paid plan
	// was planned as a replacement). Compared with the state's plan, or the one
	// its tier stands for (state written before #2143).
	if set(plan.Plan) && plan.Plan.ValueString() != planOrTier(state.Plan.ValueString(), state.Tier.ValueString()) {
		v := plan.Plan.ValueString()
		upd.Plan = &v
	}

	updated, err := r.client.UpdateK8sCluster(ctx, id, upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update K8s cluster", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, updated, &plan)
	warnDiags(&resp.Diagnostics, diags)
	resp.Diagnostics.Append(r.applyMaintenance(ctx, id, &newState, config.Maintenance, state.Maintenance, plan.Maintenance)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

// reconcileInitialPool applies the in-place changes of `initial_pool`:
// replicas, autoscaler bounds, labels and taints by PATCH; a raised
// k8s_version by the pool upgrade route (#2143), waited for. A k8s_version
// below the version the pool runs is not applied (warning).
func (r *k8sResource) reconcileInitialPool(ctx context.Context, clusterID string, ip, sp *initialPoolModel) diag.Diagnostics {
	var diags diag.Diagnostics
	patch := !ip.Replicas.Equal(sp.Replicas) || !ip.MinSize.Equal(sp.MinSize) || !ip.MaxSize.Equal(sp.MaxSize) ||
		!ip.Labels.Equal(sp.Labels) || !ip.Taints.Equal(sp.Taints)
	version := !ip.K8sVersion.Equal(sp.K8sVersion) && set(ip.K8sVersion)
	if !patch && !version {
		return diags
	}
	pools, err := r.client.ListK8sNodePools(ctx, clusterID)
	if err != nil {
		diags.AddError("Failed to list node pools for initial pool reconcile", err.Error())
		return diags
	}
	var pool *client.K8sNodePool
	for i := range pools {
		if pools[i].Name == ip.Name.ValueString() {
			pool = &pools[i]
			break
		}
	}
	if pool == nil {
		diags.AddError("Initial node pool not found",
			fmt.Sprintf("No node pool named %q on cluster %s", ip.Name.ValueString(), clusterID))
		return diags
	}

	if patch {
		npReq := client.K8sNodePoolUpdateRequest{}
		rep := int(ip.Replicas.ValueInt64())
		npReq.Replicas = &rep
		// Retirer min/max (null) → on envoie explicitement 0 : annotations
		// autoscaler min=0/max=0 = autoscaler désactivé, le pool reste à
		// `replicas`. Set min/max → autoscaler activé. C'est le toggle par
		// présence, aligné sur ccp_k8s_node_pool (le backend PATCH ne peut
		// pas effacer un None, mais applique 0).
		minZ := 0
		if !ip.MinSize.IsNull() && !ip.MinSize.IsUnknown() {
			minZ = int(ip.MinSize.ValueInt64())
		}
		npReq.MinSize = &minZ
		maxZ := 0
		if !ip.MaxSize.IsNull() && !ip.MaxSize.IsUnknown() {
			maxZ = int(ip.MaxSize.ValueInt64())
		}
		npReq.MaxSize = &maxZ
		// Labels + taints : on envoie toujours la valeur du plan (map/slice
		// vides si retirés → le PATCH efface), parité avec ccp_k8s_node_pool.
		labels := initialPoolLabels(ctx, ip)
		if labels == nil {
			labels = map[string]string{}
		}
		npReq.Labels = labels
		taints, err := initialPoolTaints(ctx, ip)
		if err != nil {
			diags.AddError("Invalid initial_pool taints", err.Error())
			return diags
		}
		if taints == nil {
			taints = []client.NodePoolTaint{}
		}
		npReq.Taints = taints
		if _, err := r.client.UpdateK8sNodePool(ctx, clusterID, pool.ID, npReq); err != nil {
			diags.AddError("Failed to update initial node pool", err.Error())
			return diags
		}
	}

	if version {
		target := ip.K8sVersion.ValueString()
		actual := ""
		if pool.K8sVersion != nil {
			actual = *pool.K8sVersion
		} else {
			cl, err := r.client.GetK8sCluster(ctx, clusterID)
			if err != nil {
				diags.AddError("Failed to read K8s cluster", err.Error())
				return diags
			}
			actual = cl.K8sVersion
		}
		if ccks.IsAbove(target, actual) {
			if _, err := ccks.UpgradePool(ctx, r.client, clusterID, pool.ID, ccks.Normalize(target), poolUpgradeTime); err != nil {
				diags.AddError("Failed to upgrade the initial node pool", err.Error())
				return diags
			}
		} else if c, ok := ccks.Compare(target, actual); ok && c < 0 {
			diags.AddWarning("Initial node pool not downgraded",
				fmt.Sprintf("The initial pool runs Kubernetes %s, above the configured initial_pool.k8s_version %s. "+
					"The provider never downgrades a pool: the configured value is recorded, the pool is left as is.",
					actual, target))
		}
	}
	return diags
}

func (r *k8sResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state k8sResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteK8sCluster(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete K8s cluster", err.Error())
		return
	}
	// Attendre la suppression RÉELLE avant de rendre la main. Le backend fait
	// le teardown en asynchrone (cleanup namespace + proxy, plusieurs minutes)
	// et garde la row en `deleting` jusqu'à la fin. Sans ce wait, un replace
	// (destroy-then-create avec le même nom) repart en Create pendant que
	// l'ancien cluster existe encore → `POST /v1/k8s/clusters` renvoie 409
	// "un cluster nommé X existe déjà". Polling jusqu'à 404 = nom libéré.
	if err := client.PollUntilDeleted(ctx, 20*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetK8sCluster(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm K8s cluster deletion", err.Error())
		return
	}
}

func (r *k8sResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func pollUntilReady(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.K8sCluster, error) {
	deadline := time.Now().Add(timeout)
	for {
		cluster, err := c.GetK8sCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		switch cluster.Status {
		case client.K8sClusterStatusActive:
			return cluster, nil
		case client.K8sClusterStatusError:
			msg := "unknown"
			if cluster.ErrorMessage != nil {
				msg = *cluster.ErrorMessage
			}
			return cluster, fmt.Errorf("cluster entered error state: %s", msg)
		}
		if time.Now().After(deadline) {
			return cluster, fmt.Errorf("polling timeout (last status: %s)", cluster.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
}
