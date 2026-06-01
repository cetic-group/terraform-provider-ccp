// Package vnetpeering implements the ccp_vnet_peering data source.
// Lookup is by `id` only — the API exposes no list endpoint.
package vnetpeering

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
	_ datasource.DataSource              = (*peeringDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*peeringDataSource)(nil)
)

func New() datasource.DataSource { return &peeringDataSource{} }

type peeringDataSource struct{ client *client.Client }

type peeringDSModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	VnetAID      types.String `tfsdk:"vnet_a_id"`
	VnetBID      types.String `tfsdk:"vnet_b_id"`
	Status       types.String `tfsdk:"status"`
	ErrorMessage types.String `tfsdk:"error_message"`
	Tags         types.List   `tfsdk:"tags"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (d *peeringDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vnet_peering"
}

func (d *peeringDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a VNet peering by `id`. No list endpoint is exposed.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Required: true},
			"name":          schema.StringAttribute{Computed: true},
			"vnet_a_id":     schema.StringAttribute{Computed: true},
			"vnet_b_id":     schema.StringAttribute{Computed: true},
			"status":        schema.StringAttribute{Computed: true},
			"error_message": schema.StringAttribute{Computed: true},
			"tags":          schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":    schema.StringAttribute{Computed: true},
		},
	}
}

func (d *peeringDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *peeringDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg peeringDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := d.client.GetVnetPeering(ctx, cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read VNet peering", err.Error())
		return
	}
	state := peeringDSModel{
		ID:        types.StringValue(got.ID),
		Name:      types.StringValue(got.Name),
		VnetAID:   types.StringValue(got.VnetAID),
		VnetBID:   types.StringValue(got.VnetBID),
		Status:    types.StringValue(got.Status),
		CreatedAt: types.StringValue(got.CreatedAt.Format(time.RFC3339)),
	}
	if got.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*got.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, got.Tags)
	state.Tags = tags
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
