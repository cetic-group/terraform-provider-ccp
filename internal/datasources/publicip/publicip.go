// Package publicip implements the ccp_public_ip data source.
package publicip

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*pipDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*pipDataSource)(nil)
)

func New() datasource.DataSource { return &pipDataSource{} }

type pipDataSource struct{ client *client.Client }

type pipDSModel struct {
	ID               types.String `tfsdk:"id"`
	IPAddress        types.String `tfsdk:"ip_address"`
	PoolID           types.String `tfsdk:"pool_id"`
	Region           types.String `tfsdk:"region"`
	Status           types.String `tfsdk:"status"`
	ContainerID      types.String `tfsdk:"container_id"`
	VMInstanceID     types.String `tfsdk:"vm_instance_id"`
	LoadBalancerID   types.String `tfsdk:"load_balancer_id"`
	LoadBalancerName types.String `tfsdk:"load_balancer_name"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (d *pipDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_public_ip"
}

func (d *pipDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a Public IP by `id`, or by `ip_address`.",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Optional: true, Computed: true},
			"ip_address":         schema.StringAttribute{Optional: true, Computed: true},
			"pool_id":            schema.StringAttribute{Computed: true},
			"region":             schema.StringAttribute{Computed: true},
			"status":             schema.StringAttribute{Computed: true},
			"container_id":       schema.StringAttribute{Computed: true},
			"vm_instance_id":     schema.StringAttribute{Computed: true},
			"load_balancer_id":   schema.StringAttribute{Computed: true},
			"load_balancer_name": schema.StringAttribute{Computed: true},
			"created_at":         schema.StringAttribute{Computed: true},
		},
	}
}

func (d *pipDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *pipDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg pipDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasAddr := !cfg.IPAddress.IsNull() && !cfg.IPAddress.IsUnknown() && cfg.IPAddress.ValueString() != ""

	if (hasID && hasAddr) || (!hasID && !hasAddr) {
		resp.Diagnostics.AddError("Lookup arguments", "Provide exactly one of `id` or `ip_address`.")
		return
	}

	list, err := d.client.ListPublicIPs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list public IPs", err.Error())
		return
	}

	var found *client.PublicIP
	for i := range list {
		if (hasID && list[i].ID == cfg.ID.ValueString()) || (hasAddr && list[i].IPAddress == cfg.IPAddress.ValueString()) {
			found = &list[i]
			break
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Public IP not found", "No matching public IP.")
		return
	}

	state := pipDSModel{
		ID:        types.StringValue(found.ID),
		IPAddress: types.StringValue(found.IPAddress),
		PoolID:    types.StringValue(found.PoolID),
		Region:    types.StringValue(found.Region),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	setStrPtr(&state.ContainerID, found.ContainerID)
	setStrPtr(&state.VMInstanceID, found.VMInstanceID)
	setStrPtr(&state.LoadBalancerID, found.LoadBalancerID)
	setStrPtr(&state.LoadBalancerName, found.LoadBalancerName)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func setStrPtr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}
