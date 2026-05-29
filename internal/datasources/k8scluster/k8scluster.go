// Package k8scluster implements the ccp_k8s_cluster data source — look up
// an existing CETIC Cloud Kubernetes (CCKS) cluster by `id` or by the
// unique `(name, region)` pair.
//
// All Computed fields of the resource counterpart are exposed, including
// the `tier` HA topology selector and the read-only `proxy_secondary_*` /
// `proxy_vip_vnet` fields added in provider v0.21.0.
package k8scluster

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
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
	// Tier (HA topology)
	Tier               types.String `tfsdk:"tier"`
	ProxySecondaryVmid types.Int64  `tfsdk:"proxy_secondary_vmid"`
	ProxySecondaryNode types.String `tfsdk:"proxy_secondary_node"`
	ProxyVipVnet       types.String `tfsdk:"proxy_vip_vnet"`
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
			"tier": schema.StringAttribute{
				MarkdownDescription: "Topology of the LXC proxy fronting the apiserver. `dev` = single proxy, " +
					"`prod` = 2 proxies with Keepalived VRRP + floating VIP (HA).",
				Computed: true,
			},
			"proxy_secondary_vmid": schema.Int64Attribute{
				MarkdownDescription: "Proxmox VMID of the secondary LXC proxy (tier `prod` only).",
				Computed:            true,
			},
			"proxy_secondary_node": schema.StringAttribute{
				MarkdownDescription: "Proxmox node hosting the secondary LXC proxy (tier `prod` only).",
				Computed:            true,
			},
			"proxy_vip_vnet": schema.StringAttribute{
				MarkdownDescription: "Keepalived VRRP floating VIP shared between the LXC proxies (tier `prod` only).",
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

	// Tier defaults to "dev" for legacy rows predating the HA backend migration.
	tier := found.Tier
	if tier == "" {
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
		Tier:                             types.StringValue(tier),
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
	if found.ProxySecondaryVmid != nil {
		state.ProxySecondaryVmid = types.Int64Value(*found.ProxySecondaryVmid)
	} else {
		state.ProxySecondaryVmid = types.Int64Null()
	}
	if found.ProxySecondaryNode != nil {
		state.ProxySecondaryNode = types.StringValue(*found.ProxySecondaryNode)
	} else {
		state.ProxySecondaryNode = types.StringNull()
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

	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
