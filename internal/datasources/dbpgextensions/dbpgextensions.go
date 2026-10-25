// Package dbpgextensions implements the ccp_db_pg_extensions data source:
// the PostgreSQL extensions that can be allowed on an instance of a given
// major version.
package dbpgextensions

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*pgExtensionsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*pgExtensionsDataSource)(nil)
)

func New() datasource.DataSource { return &pgExtensionsDataSource{} }

type pgExtensionsDataSource struct{ client *client.Client }

type pgExtensionsModel struct {
	Version          types.Int64      `tfsdk:"version"`
	Extensions       []extensionModel `tfsdk:"extensions"`
	PreloadLibraries types.List       `tfsdk:"preload_libraries"`
	DirectExtensions types.List       `tfsdk:"direct_extensions"`
}

type extensionModel struct {
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	RequiresPreload types.Bool   `tfsdk:"requires_preload"`
	Requires        types.List   `tfsdk:"requires"`
	SingleDatabase  types.Bool   `tfsdk:"single_database"`
}

// defaultVersion is the platform's default PostgreSQL major version.
const defaultVersion = 16

func (d *pgExtensionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_db_pg_extensions"
}

func (d *pgExtensionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Extension catalog of a PostgreSQL major version: the extensions that " +
			"can be allowed on a `ccp_db_pg_instance` (`allowed_extensions`), the libraries that can " +
			"be preloaded, and the extensions the database owner can create directly, without " +
			"authorization.",
		Attributes: map[string]schema.Attribute{
			"version": schema.Int64Attribute{
				MarkdownDescription: "PostgreSQL major version. Defaults to `16`.",
				Optional:            true,
				Computed:            true,
			},
			"extensions": schema.ListNestedAttribute{
				MarkdownDescription: "Extensions that can be listed in `allowed_extensions`.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Extension name, as used in `CREATE EXTENSION`.",
							Computed:            true,
						},
						"description": schema.StringAttribute{Computed: true},
						"requires_preload": schema.BoolAttribute{
							MarkdownDescription: "The extension must also be listed in `preload_libraries`.",
							Computed:            true,
						},
						"requires": schema.ListAttribute{
							MarkdownDescription: "Extensions it depends on, to list in `allowed_extensions` as well.",
							ElementType:         types.StringType,
							Computed:            true,
						},
						"single_database": schema.BoolAttribute{
							MarkdownDescription: "The extension works in a single database of the instance (see `cron_database`).",
							Computed:            true,
						},
					},
				},
			},
			"preload_libraries": schema.ListAttribute{
				MarkdownDescription: "Libraries that can be listed in `preload_libraries`.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"direct_extensions": schema.ListAttribute{
				MarkdownDescription: "Extensions the database owner can create directly with `CREATE EXTENSION`, " +
					"without listing them in `allowed_extensions`.",
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

func (d *pgExtensionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *pgExtensionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg pgExtensionsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	version := int64(defaultVersion)
	if !cfg.Version.IsNull() && !cfg.Version.IsUnknown() {
		version = cfg.Version.ValueInt64()
	}
	cat, err := d.client.GetDbPgExtensionCatalog(ctx, int(version))
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("No extension catalog",
				fmt.Sprintf("No extension can be allowed on PostgreSQL %d.", version))
			return
		}
		resp.Diagnostics.AddError("Failed to read the PostgreSQL extension catalog", err.Error())
		return
	}
	state := pgExtensionsModel{
		Version:    types.Int64Value(version),
		Extensions: make([]extensionModel, 0, len(cat.Extensions)),
	}
	for _, e := range cat.Extensions {
		requires, _ := types.ListValueFrom(ctx, types.StringType, orEmpty(e.Requires))
		state.Extensions = append(state.Extensions, extensionModel{
			Name:            types.StringValue(e.Name),
			Description:     types.StringValue(e.Description),
			RequiresPreload: types.BoolValue(e.RequiresPreload),
			Requires:        requires,
			SingleDatabase:  types.BoolValue(e.SingleDatabase),
		})
	}
	state.PreloadLibraries, _ = types.ListValueFrom(ctx, types.StringType, orEmpty(cat.PreloadLibraries))
	state.DirectExtensions, _ = types.ListValueFrom(ctx, types.StringType, orEmpty(cat.DirectExtensions))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
