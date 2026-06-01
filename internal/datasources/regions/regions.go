package regions

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure interface compliance.
var (
	_ datasource.DataSource              = &regionsDataSource{}
	_ datasource.DataSourceWithConfigure = &regionsDataSource{}
)

// New returns a new instance of the ccp_regions data source.
func New() datasource.DataSource {
	return &regionsDataSource{}
}

// regionsDataSource is the data source implementation.
type regionsDataSource struct {
	client *client.Client
}

// regionsDataSourceModel maps the data source schema state.
type regionsDataSourceModel struct {
	Regions []regionModel `tfsdk:"regions"`
}

// regionModel maps a single region object in state.
type regionModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Location  types.String `tfsdk:"location"`
	Country   types.String `tfsdk:"country"`
	Flag      types.String `tfsdk:"flag"`
	Available types.Bool   `tfsdk:"available"`
}

// Metadata returns the data source type name.
func (d *regionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_regions"
}

// Schema defines the schema for the data source.
func (d *regionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all CETIC Cloud regions available to the authenticated tenant.",
		Attributes: map[string]schema.Attribute{
			"regions": schema.ListNestedAttribute{
				Description: "List of available CETIC Cloud regions.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Region identifier (e.g. RNN, PAR, ABJ).",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Human-readable region name.",
							Computed:    true,
						},
						"location": schema.StringAttribute{
							Description: "City or geographic location of the region.",
							Computed:    true,
						},
						"country": schema.StringAttribute{
							Description: "Country in which the region is hosted.",
							Computed:    true,
						},
						"flag": schema.StringAttribute{
							Description: "Country flag emoji or code for display purposes.",
							Computed:    true,
						},
						"available": schema.BoolAttribute{
							Description: "Whether the region is currently available for provisioning.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider-configured client to the data source.
func (d *regionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = c
}

// Read fetches the list of regions from the CETIC Cloud API.
func (d *regionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	regions, err := d.client.ListRegions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Read CETIC Cloud Regions",
			"An error occurred while fetching the list of regions: "+err.Error(),
		)
		return
	}

	state := regionsDataSourceModel{
		Regions: make([]regionModel, 0, len(regions)),
	}

	for _, r := range regions {
		state.Regions = append(state.Regions, regionModel{
			ID:        types.StringValue(r.ID),
			Name:      types.StringValue(r.Name),
			Location:  types.StringValue(r.Location),
			Country:   types.StringValue(r.Country),
			Flag:      types.StringValue(r.Flag),
			Available: types.BoolValue(r.Available),
		})
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
