// Package vpc implements the ccp_vpc data source — look up an existing
// VPC by `id`, or by the unique `(name, region)` pair.
package vpc

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
	_ datasource.DataSource              = (*vpcDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*vpcDataSource)(nil)
)

func New() datasource.DataSource { return &vpcDataSource{} }

type vpcDataSource struct{ client *client.Client }

type vpcDSModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Region    types.String `tfsdk:"region"`
	Cidr      types.String `tfsdk:"cidr"`
	VlanID    types.Int64  `tfsdk:"vlan_id"`
	SDNType   types.String `tfsdk:"sdn_type"`
	Status    types.String `tfsdk:"status"`
	Tags      types.List   `tfsdk:"tags"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (d *vpcDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vpc"
}

func (d *vpcDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing VPC by `id`, or by the unique `(name, region)` pair.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VPC. Conflicts with `name` + `region`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the VPC. Combined with `region` to identify it.",
				Optional:            true,
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region of the VPC. Required when looking up by `name`.",
				Optional:            true,
				Computed:            true,
			},
			"cidr":       schema.StringAttribute{Computed: true},
			"vlan_id":    schema.Int64Attribute{Computed: true},
			"sdn_type":   schema.StringAttribute{Computed: true},
			"status":     schema.StringAttribute{Computed: true},
			"tags":       schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *vpcDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vpcDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg vpcDSModel
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

	var found *client.VPC
	if hasID {
		got, err := d.client.GetVPC(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read VPC", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListVPCs(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list VPCs", err.Error())
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
			resp.Diagnostics.AddError("VPC not found",
				fmt.Sprintf("No VPC named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple VPCs matched",
				fmt.Sprintf("Found %d VPCs named %q in region %q.", len(matches), wantName, wantRegion))
			return
		}
	}

	state := vpcDSModel{
		ID:        types.StringValue(found.ID),
		Name:      types.StringValue(found.Name),
		Region:    types.StringValue(found.Region),
		SDNType:   types.StringValue(found.SDNType),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	if found.CIDR != nil {
		state.Cidr = types.StringValue(*found.CIDR)
	} else {
		state.Cidr = types.StringNull()
	}
	if found.VlanID != nil {
		state.VlanID = types.Int64Value(int64(*found.VlanID))
	} else {
		state.VlanID = types.Int64Null()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
