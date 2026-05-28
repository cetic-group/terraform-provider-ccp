// Package loadbalancer implements the ccp_load_balancer data source.
package loadbalancer

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*lbDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*lbDataSource)(nil)
)

func New() datasource.DataSource { return &lbDataSource{} }

type lbDataSource struct{ client *client.Client }

type lbDSModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	Plan            types.String `tfsdk:"plan"`
	VnetID          types.String `tfsdk:"vnet_id"`
	VIPAddress      types.String `tfsdk:"vip_address"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	Status          types.String `tfsdk:"status"`
	ErrorMessage    types.String `tfsdk:"error_message"`
	Tags            types.List   `tfsdk:"tags"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func (d *lbDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_load_balancer"
}

func (d *lbDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a Load Balancer by `id` or by `(name, region)`. Listeners + backends are not surfaced — manage them via dedicated resources.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true},
			"name":              schema.StringAttribute{Optional: true, Computed: true},
			"region":            schema.StringAttribute{Optional: true, Computed: true},
			"plan":              schema.StringAttribute{Computed: true},
			"vnet_id":           schema.StringAttribute{Computed: true},
			"vip_address":       schema.StringAttribute{Computed: true},
			"public_ip_address": schema.StringAttribute{Computed: true},
			"public_ip_id":      schema.StringAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
			"error_message":     schema.StringAttribute{Computed: true},
			"tags":              schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":        schema.StringAttribute{Computed: true},
			"updated_at":        schema.StringAttribute{Computed: true},
		},
	}
}

func (d *lbDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *lbDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg lbDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	hasRegion := !cfg.Region.IsNull() && !cfg.Region.IsUnknown() && cfg.Region.ValueString() != ""

	switch {
	case hasID && (hasName || hasRegion):
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id`, or both `name` and `region`.")
		return
	case !hasID && !(hasName && hasRegion):
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id`, or both `name` and `region`.")
		return
	}

	var found *client.LoadBalancer
	if hasID {
		got, err := d.client.GetLoadBalancer(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read load balancer", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListLoadBalancers(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list load balancers", err.Error())
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
			resp.Diagnostics.AddError("Load balancer not found", fmt.Sprintf("No load balancer named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple load balancers matched", fmt.Sprintf("Found %d load balancers named %q in region %q.", len(matches), wantName, wantRegion))
			return
		}
	}

	state := lbDSModel{
		ID:        types.StringValue(found.ID),
		Name:      types.StringValue(found.Name),
		Region:    types.StringValue(found.Region),
		Plan:      types.StringValue(found.Plan),
		VnetID:    types.StringValue(found.VnetID),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt),
		UpdatedAt: types.StringValue(found.UpdatedAt),
	}
	setStrPtr(&state.VIPAddress, found.VIPAddress)
	setStrPtr(&state.PublicIPAddress, found.PublicIPAddress)
	setStrPtr(&state.PublicIPID, found.PublicIPID)
	setStrPtr(&state.ErrorMessage, found.ErrorMessage)
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func setStrPtr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}
