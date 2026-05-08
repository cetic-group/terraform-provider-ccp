package dbengineversions

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &dbEngineVersionsDataSource{}
	_ datasource.DataSourceWithConfigure = &dbEngineVersionsDataSource{}
)

func New() datasource.DataSource { return &dbEngineVersionsDataSource{} }

type dbEngineVersionsDataSource struct {
	client *client.Client
}

type dbEngineVersionsModel struct {
	Engine   types.String   `tfsdk:"engine"`
	Versions []versionModel `tfsdk:"versions"`
}

type versionModel struct {
	Engine    types.String `tfsdk:"engine"`
	Version   types.String `tfsdk:"version"`
	Label     types.String `tfsdk:"label"`
	IsDefault types.Bool   `tfsdk:"is_default"`
}

func (d *dbEngineVersionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_db_engine_versions"
}

func (d *dbEngineVersionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists active DB engine versions exposed to clients. Filter by `engine` to get versions for a specific backend.",
		Attributes: map[string]schema.Attribute{
			"engine": schema.StringAttribute{
				Description: "Optional engine filter: `pg`, `mysql`, `valkey`, `ferretdb`. If omitted, returns versions for all engines.",
				Optional:    true,
			},
			"versions": schema.ListNestedAttribute{
				Description: "List of active engine versions, possibly filtered by engine.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"engine": schema.StringAttribute{
							Description: "Engine the version belongs to.",
							Computed:    true,
						},
						"version": schema.StringAttribute{
							Description: "Version string (used in `ccp_db_<engine>_instance.engine_version`). Example: `16` for Postgres, `2.7.0` for FerretDB.",
							Computed:    true,
						},
						"label": schema.StringAttribute{
							Description: "Optional human-readable label.",
							Computed:    true,
						},
						"is_default": schema.BoolAttribute{
							Description: "Whether this version is the default suggestion in the console for its engine.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *dbEngineVersionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *dbEngineVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg dbEngineVersionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	engine := cfg.Engine.ValueString()
	versions, err := d.client.ListDbEngineVersions(ctx, engine)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read DB engine versions", err.Error())
		return
	}
	state := dbEngineVersionsModel{
		Engine:   cfg.Engine,
		Versions: make([]versionModel, 0, len(versions)),
	}
	for _, v := range versions {
		label := types.StringNull()
		if v.Label != nil {
			label = types.StringValue(*v.Label)
		}
		state.Versions = append(state.Versions, versionModel{
			Engine:    types.StringValue(v.Engine),
			Version:   types.StringValue(v.Version),
			Label:     label,
			IsDefault: types.BoolValue(v.IsDefault),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
