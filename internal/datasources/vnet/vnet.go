// Package vnet implements the ccp_vnet data source — look up an existing
// VNet by `(id, vpc_id)` or by `(name, vpc_id)`.
package vnet

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
	_ datasource.DataSource              = (*vnetDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*vnetDataSource)(nil)
)

func New() datasource.DataSource { return &vnetDataSource{} }

type vnetDataSource struct{ client *client.Client }

type vnetDSModel struct {
	ID        types.String `tfsdk:"id"`
	VPCID     types.String `tfsdk:"vpc_id"`
	Name      types.String `tfsdk:"name"`
	CIDR      types.String `tfsdk:"cidr"`
	Gateway   types.String `tfsdk:"gateway"`
	DHCPStart types.String `tfsdk:"dhcp_start"`
	DHCPEnd   types.String `tfsdk:"dhcp_end"`
	SNAT      types.Bool   `tfsdk:"snat"`
	Isolated  types.Bool   `tfsdk:"isolated"`
	Status    types.String `tfsdk:"status"`
	Tags      types.List   `tfsdk:"tags"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (d *vnetDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vnet"
}

func (d *vnetDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing VNet under a VPC by `(id, vpc_id)` or `(name, vpc_id)`. " +
			"`vpc_id` is always required.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet. Conflicts with `name`.",
				Optional:            true,
				Computed:            true,
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent VPC.",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the VNet within the parent VPC. Conflicts with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"cidr":       schema.StringAttribute{Computed: true},
			"gateway":    schema.StringAttribute{Computed: true},
			"dhcp_start": schema.StringAttribute{Computed: true},
			"dhcp_end":   schema.StringAttribute{Computed: true},
			"snat":       schema.BoolAttribute{Computed: true},
			"isolated":   schema.BoolAttribute{Computed: true},
			"status":     schema.StringAttribute{Computed: true},
			"tags":       schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *vnetDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vnetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg vnetDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcID := cfg.VPCID.ValueString()
	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name` (with `vpc_id`), not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name` along with `vpc_id`.")
		return
	}

	var found *client.VNet
	if hasID {
		got, err := d.client.GetVNet(ctx, vpcID, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read VNet", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListVNets(ctx, vpcID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list VNets", err.Error())
			return
		}
		wantName := cfg.Name.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == wantName {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("VNet not found",
				fmt.Sprintf("No VNet named %q in VPC %q.", wantName, vpcID))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple VNets matched",
				fmt.Sprintf("Found %d VNets named %q in VPC %q.", len(matches), wantName, vpcID))
			return
		}
	}

	state := vnetDSModel{
		ID:        types.StringValue(found.ID),
		VPCID:     types.StringValue(found.VPCID),
		Name:      types.StringValue(found.Name),
		CIDR:      types.StringValue(found.CIDR),
		SNAT:      types.BoolValue(found.SNAT),
		Isolated:  types.BoolValue(found.Isolated),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	if found.Gateway != nil {
		state.Gateway = types.StringValue(*found.Gateway)
	} else {
		state.Gateway = types.StringNull()
	}
	if found.DHCPStart != nil {
		state.DHCPStart = types.StringValue(*found.DHCPStart)
	} else {
		state.DHCPStart = types.StringNull()
	}
	if found.DHCPEnd != nil {
		state.DHCPEnd = types.StringValue(*found.DHCPEnd)
	} else {
		state.DHCPEnd = types.StringNull()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
