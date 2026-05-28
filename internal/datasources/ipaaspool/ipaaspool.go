// Package ipaaspool implements the ccp_ipaas_pool data source (admin-only).
package ipaaspool

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
	_ datasource.DataSource              = (*poolDS)(nil)
	_ datasource.DataSourceWithConfigure = (*poolDS)(nil)
)

func New() datasource.DataSource { return &poolDS{} }

type poolDS struct{ client *client.Client }

type poolDSModel struct {
	ID        types.String `tfsdk:"id"`
	Region    types.String `tfsdk:"region"`
	CIDR      types.String `tfsdk:"cidr"`
	Gateway   types.String `tfsdk:"gateway"`
	Kind      types.String `tfsdk:"kind"`
	EdgeID    types.String `tfsdk:"edge_id"`
	IsActive  types.Bool   `tfsdk:"is_active"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (d *poolDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_ipaas_pool"
}

func (d *poolDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an IPaaS pool (admin-managed) by `id`.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Required: true},
			"region":     schema.StringAttribute{Computed: true},
			"cidr":       schema.StringAttribute{Computed: true},
			"gateway":    schema.StringAttribute{Computed: true},
			"kind":       schema.StringAttribute{Computed: true},
			"edge_id":    schema.StringAttribute{Computed: true},
			"is_active":  schema.BoolAttribute{Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *poolDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *poolDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg poolDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := d.client.GetIpaasPool(ctx, cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read IPaaS pool", err.Error())
		return
	}
	state := poolDSModel{
		ID:        types.StringValue(got.ID),
		Region:    types.StringValue(got.Region),
		CIDR:      types.StringValue(got.CIDR),
		Gateway:   types.StringValue(got.Gateway),
		Kind:      types.StringValue(got.Kind),
		IsActive:  types.BoolValue(got.IsActive),
		CreatedAt: types.StringValue(got.CreatedAt.Format(time.RFC3339)),
	}
	if got.EdgeID != nil {
		state.EdgeID = types.StringValue(*got.EdgeID)
	} else {
		state.EdgeID = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
