package containertemplates

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &containerTemplatesDataSource{}
	_ datasource.DataSourceWithConfigure = &containerTemplatesDataSource{}
)

func New() datasource.DataSource { return &containerTemplatesDataSource{} }

type containerTemplatesDataSource struct {
	client *client.Client
}

type containerTemplatesModel struct {
	Templates []containerTemplateModel `tfsdk:"templates"`
}

type containerTemplateModel struct {
	Key         types.String `tfsdk:"key"`
	DisplayName types.String `tfsdk:"display_name"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

func (d *containerTemplatesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_container_templates"
}

func (d *containerTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists active container templates available on CETIC Cloud (admin-managed catalog). Useful for resolving a template `key` (e.g. `ubuntu-24.04`) to use in `ccp_container_instance.template`.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				Description: "List of active container templates.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							Description: "Template key (used in `ccp_container_instance.template`). Example: `ubuntu-24.04`.",
							Computed:    true,
						},
						"display_name": schema.StringAttribute{
							Description: "Human-readable template name.",
							Computed:    true,
						},
						"is_default": schema.BoolAttribute{
							Description: "Whether this template is the default suggestion in the console.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *containerTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *containerTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	tpls, err := d.client.ListLxcTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read container templates", err.Error())
		return
	}
	state := containerTemplatesModel{Templates: make([]containerTemplateModel, 0, len(tpls))}
	for _, t := range tpls {
		state.Templates = append(state.Templates, containerTemplateModel{
			Key:         types.StringValue(t.Key),
			DisplayName: types.StringValue(t.DisplayName),
			IsDefault:   types.BoolValue(t.IsDefault),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
