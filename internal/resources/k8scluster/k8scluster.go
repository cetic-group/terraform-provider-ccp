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

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
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
	Name     types.String `tfsdk:"name"`
	Plan     types.String `tfsdk:"plan"`
	Replicas types.Int64  `tfsdk:"replicas"`
}

type k8sResourceModel struct {
	ID            types.String      `tfsdk:"id"`
	Name          types.String      `tfsdk:"name"`
	DisplayName   types.String      `tfsdk:"display_name"`
	Region        types.String      `tfsdk:"region"`
	K8sVersion    types.String      `tfsdk:"k8s_version"`
	OsTemplateKey types.String      `tfsdk:"os_template_key"`
	VpcID         types.String      `tfsdk:"vpc_id"`
	VnetID        types.String      `tfsdk:"vnet_id"`
	PodCIDR       types.String      `tfsdk:"pod_cidr"`
	ServiceCIDR   types.String      `tfsdk:"service_cidr"`
	InitialPool   *initialPoolModel `tfsdk:"initial_pool"`
	// Autoscaler timers (mutable)
	AutoscalerScaleDownDelayAfterAdd types.String `tfsdk:"autoscaler_scale_down_delay_after_add"`
	AutoscalerScaleDownUnneededTime  types.String `tfsdk:"autoscaler_scale_down_unneeded_time"`
	// Ingress controller (mutable)
	IngressControllerEnabled  types.Bool   `tfsdk:"ingress_controller_enabled"`
	IngressControllerScope    types.String `tfsdk:"ingress_controller_scope"`
	IngressControllerClass    types.String `tfsdk:"ingress_controller_class"`
	IngressPublicIPID         types.String `tfsdk:"ingress_public_ip_id"`
	IngressInternalIP         types.String `tfsdk:"ingress_internal_ip"`
	IngressPublicIPAddress    types.String `tfsdk:"ingress_public_ip_address"`
	// Apiserver IP (create-time only)
	ApiserverPublicIPID  types.String `tfsdk:"apiserver_public_ip_id"`
	ApiserverInternalIP  types.String `tfsdk:"apiserver_internal_ip"`
	// Apiserver public IP (attach/detach, mutable)
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
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
			// ── Apiserver IP (create-time) ──────────────────────────────────
			"apiserver_public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID d'une IP publique pour l'apiserver (attachée automatiquement après provisioning). Forces replacement.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"apiserver_internal_ip": schema.StringAttribute{
				MarkdownDescription: "IP privée VNet pour l'endpoint apiserver. Forces replacement.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			// ── Apiserver public IP (attach/detach, mutable) ───────────────
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID d'une IP publique attachée à l'apiserver du cluster. Mutable (attach/detach).",
				Optional:            true,
				Computed:            true,
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
				MarkdownDescription: "Initial worker pool created with the cluster (default: name=`default`, plan=`small`, replicas=1). Forces replacement.",
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
	if c.PublicIPID != nil {
		m.PublicIPID = types.StringValue(*c.PublicIPID)
	} else {
		m.PublicIPID = types.StringNull()
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
	}

	// `tier` is Optional+Computed with Default("dev"). Forward whatever the
	// plan resolved to so the backend sees an explicit value; if the framework
	// leaves it Unknown/Null (defensive guard), omit and let the API default.
	tier := ""
	if !plan.Tier.IsNull() && !plan.Tier.IsUnknown() {
		tier = plan.Tier.ValueString()
	}

	createReq := client.K8sClusterCreateRequest{
		Name:          plan.Name.ValueString(),
		Region:        plan.Region.ValueString(),
		K8sVersion:    plan.K8sVersion.ValueString(),
		OsTemplateKey: plan.OsTemplateKey.ValueString(),
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
	if !plan.ApiserverPublicIPID.IsNull() && !plan.ApiserverPublicIPID.IsUnknown() {
		v := plan.ApiserverPublicIPID.ValueString()
		createReq.ApiserverPublicIPID = &v
	}
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

	state, diags := stateFromAPI(ctx, final, plan.InitialPool)
	// Keep create-time only fields not returned by GET
	state.ApiserverPublicIPID = plan.ApiserverPublicIPID
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
	// Preserve create-time only fields not in GET response
	newState.ApiserverPublicIPID = state.ApiserverPublicIPID
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
		newState.ApiserverPublicIPID = state.ApiserverPublicIPID
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

	// ── Apiserver public IP attach/detach ───────────────────────────────────
	planIPID := plan.PublicIPID.ValueString()
	stateIPID := state.PublicIPID.ValueString()
	if planIPID != stateIPID {
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
	newState.ApiserverPublicIPID = state.ApiserverPublicIPID
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
