// Package k8scluster implements the ccp_k8s_cluster data source — look up
// an existing CETIC Cloud Kubernetes (CCKS) cluster by `id` or by the
// unique `(name, region)` pair.
//
// All Computed fields of the resource counterpart are exposed, including
// the control-plane `plan` (and its deprecated alias `tier`), the read-only
// `proxy_vip_vnet` field, and the upgrade status of #2143
// (`patch_disponible`, `upgrade_en_cours`).
package k8scluster

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*k8sClusterDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*k8sClusterDataSource)(nil)
)

func New() datasource.DataSource { return &k8sClusterDataSource{} }

type k8sClusterDataSource struct{ client *client.Client }

type k8sClusterDSModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	DisplayName         types.String `tfsdk:"display_name"`
	Region              types.String `tfsdk:"region"`
	K8sVersion          types.String `tfsdk:"k8s_version"`
	OsTemplateKey       types.String `tfsdk:"os_template_key"`
	VpcID               types.String `tfsdk:"vpc_id"`
	VnetID              types.String `tfsdk:"vnet_id"`
	PodCIDR             types.String `tfsdk:"pod_cidr"`
	ServiceCIDR         types.String `tfsdk:"service_cidr"`
	ApiEndpoint         types.String `tfsdk:"api_endpoint"`
	ApiserverPublicIPID types.String `tfsdk:"apiserver_public_ip_id"`
	PublicIPAddress     types.String `tfsdk:"public_ip_address"`
	// Autoscaler
	AutoscalerScaleDownDelayAfterAdd types.String `tfsdk:"autoscaler_scale_down_delay_after_add"`
	AutoscalerScaleDownUnneededTime  types.String `tfsdk:"autoscaler_scale_down_unneeded_time"`
	// Ingress controller
	IngressControllerEnabled types.Bool   `tfsdk:"ingress_controller_enabled"`
	IngressControllerScope   types.String `tfsdk:"ingress_controller_scope"`
	IngressControllerClass   types.String `tfsdk:"ingress_controller_class"`
	IngressPublicIPID        types.String `tfsdk:"ingress_public_ip_id"`
	IngressPublicIPAddress   types.String `tfsdk:"ingress_public_ip_address"`
	IngressInternalIP        types.String `tfsdk:"ingress_internal_ip"`
	// Plan of the control plane (#2143) and its deprecated alias tier
	Plan            types.String `tfsdk:"plan"`
	Tier            types.String `tfsdk:"tier"`
	EtcdVolumeGB    types.Int64  `tfsdk:"etcd_volume_gb"`
	NodeIPBlockSize types.Int64  `tfsdk:"node_ip_block_size"`
	ProxyVipVnet    types.String `tfsdk:"proxy_vip_vnet"`
	OsImage         types.String `tfsdk:"os_image"`
	// Upgrades (#2143)
	PatchDisponible types.Object `tfsdk:"patch_disponible"`
	UpgradeEnCours  types.Object `tfsdk:"upgrade_en_cours"`
	OIDCIssuerURL   types.String `tfsdk:"oidc_issuer_url"`
	// Lifecycle
	Status       types.String `tfsdk:"status"`
	ErrorMessage types.String `tfsdk:"error_message"`
	Tags         types.List   `tfsdk:"tags"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func (d *k8sClusterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_k8s_cluster"
}

func (d *k8sClusterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing CETIC Cloud Kubernetes cluster (CCKS) by `id` or by `(name, region)`. " +
			"Exactly one of those discriminators must be provided.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the cluster. Conflicts with `name` + `region`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the cluster. Combined with `region` to identify it.",
				Optional:            true,
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region of the cluster. Required when looking up by `name`.",
				Optional:            true,
				Computed:            true,
			},
			"display_name":                          schema.StringAttribute{Computed: true},
			"k8s_version":                           schema.StringAttribute{Computed: true},
			"os_template_key":                       schema.StringAttribute{Computed: true},
			"vpc_id":                                schema.StringAttribute{Computed: true},
			"vnet_id":                               schema.StringAttribute{Computed: true},
			"pod_cidr":                              schema.StringAttribute{Computed: true},
			"service_cidr":                          schema.StringAttribute{Computed: true},
			"api_endpoint":                          schema.StringAttribute{Computed: true},
			"apiserver_public_ip_id":                schema.StringAttribute{Computed: true},
			"public_ip_address":                     schema.StringAttribute{Computed: true},
			"autoscaler_scale_down_delay_after_add": schema.StringAttribute{Computed: true},
			"autoscaler_scale_down_unneeded_time":   schema.StringAttribute{Computed: true},
			"ingress_controller_enabled":            schema.BoolAttribute{Computed: true},
			"ingress_controller_scope":              schema.StringAttribute{Computed: true},
			"ingress_controller_class":              schema.StringAttribute{Computed: true},
			"ingress_public_ip_id":                  schema.StringAttribute{Computed: true},
			"ingress_public_ip_address":             schema.StringAttribute{Computed: true},
			"ingress_internal_ip":                   schema.StringAttribute{Computed: true},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Plan of the control plane: `starter`, `standard` or `pro`.",
				Computed:            true,
			},
			"tier": schema.StringAttribute{
				MarkdownDescription: "**Deprecated** — use `plan`. `dev` for the `starter` plan, `prod` for a paid plan.",
				DeprecationMessage:  "Use `plan` instead.",
				Computed:            true,
			},
			"etcd_volume_gb": schema.Int64Attribute{
				MarkdownDescription: "Effective etcd volume (GiB).",
				Computed:            true,
			},
			"node_ip_block_size": schema.Int64Attribute{
				MarkdownDescription: "Size of the node IP block allocated in the VNet (`null` until allocated).",
				Computed:            true,
			},
			"os_image": schema.StringAttribute{
				MarkdownDescription: "Node operating-system family (`flatcar`, `ubuntu`, `rocky10`, `rocky9`).",
				Computed:            true,
			},
			"proxy_vip_vnet": schema.StringAttribute{
				MarkdownDescription: "Floating private address shared by the two apiserver frontends (paid plans only) — the address the kubeconfig points at in HA mode.",
				Computed:            true,
			},
			"patch_disponible": schema.SingleNestedAttribute{
				MarkdownDescription: "Newest Kubernetes patch of the same minor available for the cluster, `null` when none.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"version": schema.StringAttribute{Computed: true, MarkdownDescription: "Patch version."},
					"os":      schema.StringAttribute{Computed: true, MarkdownDescription: "Node OS of the templates."},
					"templates_ok": schema.BoolAttribute{Computed: true,
						MarkdownDescription: "A template exists in that version for the OS of every pool concerned (otherwise the one-click update is refused)."},
					"plan_de_controle": schema.BoolAttribute{Computed: true,
						MarkdownDescription: "The control plane is concerned (otherwise only pools)."},
					"pools": schema.ListAttribute{Computed: true, ElementType: types.StringType,
						MarkdownDescription: "Pools concerned, updated one after the other."},
					"duree_estimee_minutes": schema.Int64Attribute{Computed: true,
						MarkdownDescription: "Estimated duration in minutes."},
				},
			},
			"upgrade_en_cours": schema.SingleNestedAttribute{
				MarkdownDescription: "Upgrade running or waiting for its window, `null` when none.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{Computed: true},
					"type": schema.StringAttribute{Computed: true,
						MarkdownDescription: "`plan_de_controle`, `pool`, `patch` or `mineure`."},
					"origine": schema.StringAttribute{Computed: true,
						MarkdownDescription: "`manuelle` or `automatique`."},
					"statut": schema.StringAttribute{Computed: true,
						MarkdownDescription: "`en_cours` or `en_pause` (automatic upgrade waiting for the next window)."},
					"version_depart": schema.StringAttribute{Computed: true},
					"version_cible":  schema.StringAttribute{Computed: true},
				},
			},
			"oidc_issuer_url": schema.StringAttribute{
				MarkdownDescription: "Issuer of the SSO OIDC configured on the apiserver, `null` without SSO.",
				Computed:            true,
			},
			"status":        schema.StringAttribute{Computed: true},
			"error_message": schema.StringAttribute{Computed: true},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"created_at": schema.StringAttribute{Computed: true},
			"updated_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *k8sClusterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *k8sClusterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg k8sClusterDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	hasRegion := !cfg.Region.IsNull() && !cfg.Region.IsUnknown() && cfg.Region.ValueString() != ""

	switch {
	case hasID && (hasName || hasRegion):
		resp.Diagnostics.AddError("Conflicting lookup arguments",
			"Provide either `id`, or both `name` and `region` — not both.")
		return
	case !hasID && !(hasName && hasRegion):
		resp.Diagnostics.AddError("Missing lookup arguments",
			"Provide either `id`, or both `name` and `region`.")
		return
	}

	var found *client.K8sCluster
	if hasID {
		got, err := d.client.GetK8sCluster(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read K8s cluster", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListK8sClusters(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list K8s clusters", err.Error())
			return
		}
		wantName, wantRegion := cfg.Name.ValueString(), cfg.Region.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == wantName && list[i].Region == wantRegion {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("K8s cluster not found",
				fmt.Sprintf("No cluster named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple K8s clusters matched",
				fmt.Sprintf("Found %d clusters named %q in region %q. Filter by `id` instead.",
					len(matches), wantName, wantRegion))
			return
		}
	}

	// Plan, derived from tier on an API older than #2143; the tier is always
	// the plan's (legacy rows without either are starter/dev).
	plan := found.Plan
	if plan == "" {
		plan = "starter"
		if found.Tier == "prod" {
			plan = "standard"
		}
	}
	tier := "prod"
	if plan == "starter" {
		tier = "dev"
	}

	state := k8sClusterDSModel{
		ID:                               types.StringValue(found.ID),
		Name:                             types.StringValue(found.Name),
		Region:                           types.StringValue(found.Region),
		K8sVersion:                       types.StringValue(found.K8sVersion),
		OsTemplateKey:                    types.StringValue(found.OsTemplateKey),
		VpcID:                            types.StringValue(found.VpcID),
		VnetID:                           types.StringValue(found.VnetID),
		PodCIDR:                          types.StringValue(found.PodCIDR),
		ServiceCIDR:                      types.StringValue(found.ServiceCIDR),
		AutoscalerScaleDownDelayAfterAdd: types.StringValue(found.AutoscalerScaleDownDelayAfterAdd),
		AutoscalerScaleDownUnneededTime:  types.StringValue(found.AutoscalerScaleDownUnneededTime),
		IngressControllerEnabled:         types.BoolValue(found.IngressControllerEnabled),
		IngressControllerScope:           types.StringValue(found.IngressControllerScope),
		IngressControllerClass:           types.StringValue(found.IngressControllerClass),
		Plan:                             types.StringValue(plan),
		Tier:                             types.StringValue(tier),
		OsImage:                          types.StringValue(found.OsImage),
		EtcdVolumeGB:                     int64OrNull(found.EtcdVolumeGB),
		NodeIPBlockSize:                  int64OrNull(found.NodeIPBlockSize),
		PatchDisponible:                  patchValue(found.PatchDisponible),
		UpgradeEnCours:                   upgradeValue(found.UpgradeEnCours),
		OIDCIssuerURL:                    types.StringNull(),
		Status:                           types.StringValue(found.Status),
		CreatedAt:                        types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt:                        types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}

	if found.DisplayName != nil {
		state.DisplayName = types.StringValue(*found.DisplayName)
	} else {
		state.DisplayName = types.StringNull()
	}
	if found.ApiEndpoint != nil {
		state.ApiEndpoint = types.StringValue(*found.ApiEndpoint)
	} else {
		state.ApiEndpoint = types.StringNull()
	}
	if found.PublicIPID != nil {
		state.ApiserverPublicIPID = types.StringValue(*found.PublicIPID)
	} else {
		state.ApiserverPublicIPID = types.StringNull()
	}
	if found.PublicIPAddress != nil {
		state.PublicIPAddress = types.StringValue(*found.PublicIPAddress)
	} else {
		state.PublicIPAddress = types.StringNull()
	}
	if found.IngressPublicIPID != nil {
		state.IngressPublicIPID = types.StringValue(*found.IngressPublicIPID)
	} else {
		state.IngressPublicIPID = types.StringNull()
	}
	if found.IngressPublicIPAddress != nil {
		state.IngressPublicIPAddress = types.StringValue(*found.IngressPublicIPAddress)
	} else {
		state.IngressPublicIPAddress = types.StringNull()
	}
	if found.IngressInternalIP != nil {
		state.IngressInternalIP = types.StringValue(*found.IngressInternalIP)
	} else {
		state.IngressInternalIP = types.StringNull()
	}
	if found.ProxyVipVnet != nil {
		state.ProxyVipVnet = types.StringValue(*found.ProxyVipVnet)
	} else {
		state.ProxyVipVnet = types.StringNull()
	}
	if found.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*found.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}

	if found.OIDC != nil {
		state.OIDCIssuerURL = types.StringValue(found.OIDC.IssuerURL)
	}

	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func int64OrNull(p *int) types.Int64 {
	if p == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*p))
}

var (
	patchAttrTypes = map[string]attr.Type{
		"version":               types.StringType,
		"os":                    types.StringType,
		"templates_ok":          types.BoolType,
		"plan_de_controle":      types.BoolType,
		"pools":                 types.ListType{ElemType: types.StringType},
		"duree_estimee_minutes": types.Int64Type,
	}
	upgradeAttrTypes = map[string]attr.Type{
		"id":             types.StringType,
		"type":           types.StringType,
		"origine":        types.StringType,
		"statut":         types.StringType,
		"version_depart": types.StringType,
		"version_cible":  types.StringType,
	}
)

func patchValue(p *client.K8sPatchDisponible) types.Object {
	if p == nil {
		return types.ObjectNull(patchAttrTypes)
	}
	pools := make([]attr.Value, 0, len(p.Pools))
	for _, n := range p.Pools {
		pools = append(pools, types.StringValue(n))
	}
	return types.ObjectValueMust(patchAttrTypes, map[string]attr.Value{
		"version":               types.StringValue(p.Version),
		"os":                    types.StringValue(p.Os),
		"templates_ok":          types.BoolValue(p.TemplatesOK),
		"plan_de_controle":      types.BoolValue(p.PlanDeControle),
		"pools":                 types.ListValueMust(types.StringType, pools),
		"duree_estimee_minutes": types.Int64Value(int64(p.DureeEstimeeMinutes)),
	})
}

func upgradeValue(u *client.K8sUpgradeOperation) types.Object {
	if u == nil {
		return types.ObjectNull(upgradeAttrTypes)
	}
	depart := types.StringNull()
	if u.VersionDepart != nil {
		depart = types.StringValue(*u.VersionDepart)
	}
	return types.ObjectValueMust(upgradeAttrTypes, map[string]attr.Value{
		"id":             types.StringValue(u.ID),
		"type":           types.StringValue(u.Type),
		"origine":        types.StringValue(u.Origine),
		"statut":         types.StringValue(u.Statut),
		"version_depart": depart,
		"version_cible":  types.StringValue(u.VersionCible),
	})
}
