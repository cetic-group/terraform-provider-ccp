// Package k8scluster implements the ccp_k8s_cluster Terraform resource.
//
// CCKS (CETIC Cloud Kubernetes Service) — cluster K8s tenant managé via
// CAPI/CAPMOX. Le pool de workers initial est créé en même temps que le
// cluster (`initial_pool` block, défaut: 1× small worker `default`).
// Pour gérer des pools additionnels, utiliser `ccp_k8s_node_pool`.
//
// Provisioning asynchrone (5-15 min). Create poll jusqu'à status=active.
// Upgrade de version = appel à /upgrade-version (rolling, pas recreation).
package k8scluster

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	// Tier (create-time only, immutable)
	Tier types.String `tfsdk:"tier"`
	// Proxy HA (read-only, only populated for tier=prod)
	ProxySecondaryVmid types.Int64  `tfsdk:"proxy_secondary_vmid"`
	ProxySecondaryNode types.String `tfsdk:"proxy_secondary_node"`
	ProxyVipVnet       types.String `tfsdk:"proxy_vip_vnet"`
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
		MarkdownDescription: "Manages a CETIC Cloud Kubernetes cluster (CCKS) provisioned via CAPI/CAPMOX. " +
			"Provisioning is asynchronous (5-15 min). Create blocks until cluster is active. " +
			"Kubernetes version upgrades are performed in-place via rolling upgrade (no recreation).",
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
				MarkdownDescription: "Kubernetes version (ex: `v1.31.0`). Mutable — triggers a rolling upgrade via CAPI/CAPMOX.",
				Required:            true,
			},
			"os_template_key": schema.StringAttribute{
				MarkdownDescription: "QEMU template key for worker nodes (ex: `clks-capi-debian-13`). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"os_image": schema.StringAttribute{
				MarkdownDescription: "Node operating-system family for the cluster nodes. One of `flatcar`, `ubuntu`, `rocky9`. " +
					"Defaults to `flatcar` when omitted. Forces replacement (changing the node OS recreates the cluster).",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("flatcar", "ubuntu", "rocky9"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
			// ── Tier (HA topology) ─────────────────────────────────────────
			"tier": schema.StringAttribute{
				MarkdownDescription: "Topologie du frontal apiserver. " +
					"`dev` (défaut) = frontal unique (SPOF acceptable en dev). " +
					"`prod` = frontal redondé (primary + secondary) avec bascule automatique sur une adresse flottante, HA au niveau du plan de contrôle. " +
					"Immutable côté backend — toute modification force la recréation du cluster.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("dev"),
				Validators: []validator.String{
					stringvalidator.OneOf("dev", "prod"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"proxy_secondary_vmid": schema.Int64Attribute{
				MarkdownDescription: "Identifiant interne du frontal apiserver secondaire (tier `prod` uniquement, sinon null). Read-only — exposé pour observabilité.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"proxy_secondary_node": schema.StringAttribute{
				MarkdownDescription: "Placement interne du frontal apiserver secondaire (tier `prod` uniquement, sinon null). Read-only — exposé pour observabilité.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"proxy_vip_vnet": schema.StringAttribute{
				MarkdownDescription: "Adresse flottante sur le VNet partagée entre les frontaux apiserver primary+secondary (tier `prod` uniquement, sinon null). C'est l'IP que le kubeconfig pointe en mode HA.",
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
						// block: stateFromAPI preserves the plan (currentInitial), there
						// is no readback, so the state must equal the config → no
						// perma-diff. Omit to inherit the cluster control-plane version.
						MarkdownDescription: "Kubernetes version of the worker nodes in the initial pool " +
							"(`vX.Y.Z`). Must be `<=` the cluster control-plane version (`k8s_version`); omit to inherit it.",
						Optional: true,
					},
					"disk_gb": schema.Int64Attribute{
						MarkdownDescription: "Root disk size (GB) of every node in the initial pool. " +
							"Optional — defaults to the pool's plan disk size when omitted. No resize " +
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

func stateFromAPI(ctx context.Context, c *client.K8sCluster, currentInitial *initialPoolModel) (k8sResourceModel, []string) {
	// Tier defaults to "dev" if the API returns it empty — covers legacy rows
	// predating the HA backend migration so the Computed schema stays coherent.
	tier := c.Tier
	if tier == "" {
		tier = "dev"
	}
	m := k8sResourceModel{
		ID:            types.StringValue(c.ID),
		Name:          types.StringValue(c.Name),
		Region:        types.StringValue(c.Region),
		K8sVersion:    types.StringValue(c.K8sVersion),
		OsTemplateKey: types.StringValue(c.OsTemplateKey),
		OsImage:       types.StringValue(c.OsImage),
		VpcID:         types.StringValue(c.VpcID),
		VnetID:        types.StringValue(c.VnetID),
		PodCIDR:       types.StringValue(c.PodCIDR),
		ServiceCIDR:   types.StringValue(c.ServiceCIDR),
		Status:        types.StringValue(c.Status),
		CreatedAt:     types.StringValue(c.CreatedAt.Format("2006-01-02T15:04:05Z07:00")),
		InitialPool:   currentInitial,
		// Autoscaler
		AutoscalerScaleDownDelayAfterAdd: types.StringValue(c.AutoscalerScaleDownDelayAfterAdd),
		AutoscalerScaleDownUnneededTime:  types.StringValue(c.AutoscalerScaleDownUnneededTime),
		// Ingress
		IngressControllerEnabled: types.BoolValue(c.IngressControllerEnabled),
		IngressControllerScope:   types.StringValue(c.IngressControllerScope),
		IngressControllerClass:   types.StringValue(c.IngressControllerClass),
		// Tier
		Tier: types.StringValue(tier),
	}

	// Proxy HA fields are only populated for tier=prod; nullable on dev.
	if c.ProxySecondaryVmid != nil {
		m.ProxySecondaryVmid = types.Int64Value(*c.ProxySecondaryVmid)
	} else {
		m.ProxySecondaryVmid = types.Int64Null()
	}
	if c.ProxySecondaryNode != nil {
		m.ProxySecondaryNode = types.StringValue(*c.ProxySecondaryNode)
	} else {
		m.ProxySecondaryNode = types.StringNull()
	}
	if c.ProxyVipVnet != nil {
		m.ProxyVipVnet = types.StringValue(*c.ProxyVipVnet)
	} else {
		m.ProxyVipVnet = types.StringNull()
	}

	// Nullable strings
	if c.DisplayName != nil {
		m.DisplayName = types.StringValue(*c.DisplayName)
	} else {
		m.DisplayName = types.StringNull()
	}
	if c.ApiEndpoint != nil {
		m.ApiEndpoint = types.StringValue(*c.ApiEndpoint)
	} else {
		m.ApiEndpoint = types.StringNull()
	}
	// Le backend expose l'IP publique apiserver via `public_ip_id` ; côté
	// provider c'est l'unique attribut `apiserver_public_ip_id` (mutable).
	if c.PublicIPID != nil {
		m.ApiserverPublicIPID = types.StringValue(*c.PublicIPID)
	} else {
		m.ApiserverPublicIPID = types.StringNull()
	}
	if c.PublicIPAddress != nil {
		m.PublicIPAddress = types.StringValue(*c.PublicIPAddress)
	} else {
		m.PublicIPAddress = types.StringNull()
	}
	if c.IngressPublicIPID != nil {
		m.IngressPublicIPID = types.StringValue(*c.IngressPublicIPID)
	} else {
		m.IngressPublicIPID = types.StringNull()
	}
	if c.IngressPublicIPAddress != nil {
		m.IngressPublicIPAddress = types.StringValue(*c.IngressPublicIPAddress)
	} else {
		m.IngressPublicIPAddress = types.StringNull()
	}
	if c.IngressInternalIP != nil {
		m.IngressInternalIP = types.StringValue(*c.IngressInternalIP)
	} else {
		m.IngressInternalIP = types.StringNull()
	}

	tags, diag := types.ListValueFrom(ctx, types.StringType, c.Tags)
	var diagStrs []string
	if diag.HasError() {
		for _, d := range diag.Errors() {
			diagStrs = append(diagStrs, d.Summary()+": "+d.Detail())
		}
	}
	m.Tags = tags
	return m, diagStrs
}

func (r *k8sResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan k8sResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
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

	// `tier` is Optional+Computed with Default("dev"). Forward whatever the
	// plan resolved to so the backend sees an explicit value; if the framework
	// leaves it Unknown/Null (defensive guard), omit and let the API default.
	tier := ""
	if !plan.Tier.IsNull() && !plan.Tier.IsUnknown() {
		tier = plan.Tier.ValueString()
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
		// Tier (HA topology) — create-time only
		Tier: tier,
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

	state, diags := stateFromAPI(ctx, final, plan.InitialPool)
	// `apiserver_internal_ip` reste create-time (non retourné par GET) ;
	// `apiserver_public_ip_id` vient désormais du read-back (stateFromAPI).
	state.ApiserverInternalIP = plan.ApiserverInternalIP
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
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
	newState, diags := stateFromAPI(ctx, got, state.InitialPool)
	// `apiserver_internal_ip` est create-time (absent du GET) → préservé ;
	// `apiserver_public_ip_id` est relu du backend par stateFromAPI.
	newState.ApiserverInternalIP = state.ApiserverInternalIP
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *k8sResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state k8sResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// ── Kubernetes version upgrade ──────────────────────────────────────────
	if !plan.K8sVersion.Equal(state.K8sVersion) {
		upgraded, err := r.client.UpgradeK8sClusterVersion(ctx, state.ID.ValueString(),
			client.K8sUpgradeVersionRequest{K8sVersion: plan.K8sVersion.ValueString()})
		if err != nil {
			resp.Diagnostics.AddError("Failed to upgrade K8s cluster version", err.Error())
			return
		}
		// Wait for upgrade to complete (20 min)
		final, err := pollUntilReady(ctx, r.client, upgraded.ID, 20*time.Minute)
		if err != nil {
			resp.Diagnostics.AddError("K8s cluster upgrade timed out or failed", err.Error())
			return
		}
		newState, diags := stateFromAPI(ctx, final, state.InitialPool)
		newState.ApiserverInternalIP = state.ApiserverInternalIP
		for _, d := range diags {
			resp.Diagnostics.AddWarning("Tags conversion warning", d)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
		// Re-read updated plan/state from the saved state and continue
		resp.Diagnostics.Append(resp.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// ── Apiserver public IP attach/detach (apiserver_public_ip_id, mutable) ──
	// Skip si la valeur planifiée est encore Unknown (known-after-apply) pour
	// ne pas détacher par erreur sur un "".
	planIPID := plan.ApiserverPublicIPID.ValueString()
	stateIPID := state.ApiserverPublicIPID.ValueString()
	if !plan.ApiserverPublicIPID.IsUnknown() && planIPID != stateIPID {
		if planIPID != "" {
			_, err := r.client.AttachIPToK8sCluster(ctx, state.ID.ValueString(),
				client.K8sAttachIPRequest{PublicIPID: planIPID})
			if err != nil {
				resp.Diagnostics.AddError("Failed to attach IP to K8s cluster", err.Error())
				return
			}
		} else {
			_, err := r.client.DetachIPFromK8sCluster(ctx, state.ID.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("Failed to detach IP from K8s cluster", err.Error())
				return
			}
		}
	}

	// ── Initial pool — réconciliation in-place (replicas + autoscaler min/max) ─
	// Le backend ne met pas à jour l'initial pool via le PATCH cluster ; il vit
	// comme un node pool. On retrouve ce node pool par son nom et on le PATCH
	// directement. Couvre le scale (replicas) ET l'activation/ajustement de
	// l'autoscaler (min/max) sur l'initial pool, sans recréer le cluster.
	if plan.InitialPool != nil && state.InitialPool != nil {
		ip, sp := plan.InitialPool, state.InitialPool
		if !ip.Replicas.Equal(sp.Replicas) || !ip.MinSize.Equal(sp.MinSize) || !ip.MaxSize.Equal(sp.MaxSize) ||
			!ip.Labels.Equal(sp.Labels) || !ip.Taints.Equal(sp.Taints) || !ip.K8sVersion.Equal(sp.K8sVersion) {
			pools, err := r.client.ListK8sNodePools(ctx, state.ID.ValueString())
			if err != nil {
				resp.Diagnostics.AddError("Failed to list node pools for initial pool reconcile", err.Error())
				return
			}
			poolID := ""
			for _, p := range pools {
				if p.Name == ip.Name.ValueString() {
					poolID = p.ID
					break
				}
			}
			if poolID == "" {
				resp.Diagnostics.AddError("Initial node pool not found",
					fmt.Sprintf("No node pool named %q on cluster %s", ip.Name.ValueString(), state.ID.ValueString()))
				return
			}
			npReq := client.K8sNodePoolUpdateRequest{}
			rep := int(ip.Replicas.ValueInt64())
			npReq.Replicas = &rep
			// k8s_version : worker version of the initial pool. Send only when set
			// (changing it triggers a rolling upgrade); omit to keep inheriting the
			// control-plane version.
			if !ip.K8sVersion.IsNull() && !ip.K8sVersion.IsUnknown() {
				v := ip.K8sVersion.ValueString()
				npReq.K8sVersion = &v
			}
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
				resp.Diagnostics.AddError("Invalid initial_pool taints", err.Error())
				return
			}
			if taints == nil {
				taints = []client.NodePoolTaint{}
			}
			npReq.Taints = taints
			if _, err := r.client.UpdateK8sNodePool(ctx, state.ID.ValueString(), poolID, npReq); err != nil {
				resp.Diagnostics.AddError("Failed to update initial node pool", err.Error())
				return
			}
		}
	}

	// ── Metadata + autoscaler + ingress update ──────────────────────────────
	var upd client.K8sClusterUpdateRequest
	if !plan.DisplayName.Equal(state.DisplayName) {
		v := plan.DisplayName.ValueString()
		upd.DisplayName = &v
	}
	if !plan.Tags.Equal(state.Tags) {
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

	updated, err := r.client.UpdateK8sCluster(ctx, state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update K8s cluster", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, updated, plan.InitialPool)
	// `apiserver_public_ip_id` reflète le backend (post-attach/détach) ;
	// `apiserver_internal_ip` reste create-time (préservé).
	newState.ApiserverInternalIP = state.ApiserverInternalIP
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
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
