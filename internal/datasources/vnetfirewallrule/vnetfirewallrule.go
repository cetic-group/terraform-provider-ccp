// Package vnetfirewallrule implements the ccp_vnet_firewall_rule data source.
package vnetfirewallrule

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*fwDS)(nil)
	_ datasource.DataSourceWithConfigure = (*fwDS)(nil)
)

func New() datasource.DataSource { return &fwDS{} }

type fwDS struct{ client *client.Client }

type fwDSModel struct {
	ID         types.String `tfsdk:"id"`
	VnetID     types.String `tfsdk:"vnet_id"`
	Position   types.Int64  `tfsdk:"position"`
	Direction  types.String `tfsdk:"direction"`
	Action     types.String `tfsdk:"action"`
	Proto      types.String `tfsdk:"proto"`
	SourceCIDR types.String `tfsdk:"source_cidr"`
	DestCIDR   types.String `tfsdk:"dest_cidr"`
	Dport      types.String `tfsdk:"dport"`
	Comment    types.String `tfsdk:"comment"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (d *fwDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vnet_firewall_rule"
}

func (d *fwDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a VNet firewall rule by `(id, vnet_id)`.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Required: true},
			"vnet_id":     schema.StringAttribute{Required: true},
			"position":    schema.Int64Attribute{Computed: true},
			"direction":   schema.StringAttribute{Computed: true},
			"action":      schema.StringAttribute{Computed: true},
			"proto":       schema.StringAttribute{Computed: true},
			"source_cidr": schema.StringAttribute{Computed: true},
			"dest_cidr":   schema.StringAttribute{Computed: true},
			"dport":       schema.StringAttribute{Computed: true},
			"comment":     schema.StringAttribute{Computed: true},
			"enabled":     schema.BoolAttribute{Computed: true},
			"created_at":  schema.StringAttribute{Computed: true},
		},
	}
}

func (d *fwDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *fwDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg fwDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := d.client.GetVnetFirewallRule(ctx, cfg.VnetID.ValueString(), cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read firewall rule", err.Error())
		return
	}
	state := fwDSModel{
		ID:        types.StringValue(got.ID),
		VnetID:    types.StringValue(got.VnetID),
		Position:  types.Int64Value(int64(got.Position)),
		Direction: types.StringValue(got.Direction),
		Action:    types.StringValue(got.Action),
		Enabled:   types.BoolValue(got.Enabled),
		CreatedAt: types.StringValue(got.CreatedAt),
	}
	setStrPtr(&state.Proto, got.Proto)
	setStrPtr(&state.SourceCIDR, got.SourceCIDR)
	setStrPtr(&state.DestCIDR, got.DestCIDR)
	setStrPtr(&state.Dport, got.Dport)
	setStrPtr(&state.Comment, got.Comment)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func setStrPtr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}
