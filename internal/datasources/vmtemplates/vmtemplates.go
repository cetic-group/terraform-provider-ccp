package vmtemplates

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &vmTemplatesDataSource{}
	_ datasource.DataSourceWithConfigure = &vmTemplatesDataSource{}
)

func New() datasource.DataSource { return &vmTemplatesDataSource{} }

type vmTemplatesDataSource struct {
	client *client.Client
}

type vmTemplatesModel struct {
	Templates []vmTemplateModel `tfsdk:"templates"`
}

type vmTemplateModel struct {
	Key         types.String `tfsdk:"key"`
	DisplayName types.String `tfsdk:"display_name"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

func (d *vmTemplatesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vm_templates"
}

func (d *vmTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists active VM templates available on CETIC Cloud (admin-managed catalog, excludes internal `ccks-*` Kubernetes images). Useful for resolving a template `key` (e.g. `ubuntu-24.04-cloud`) to use in `ccp_vm_instance.template`.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				Description: "List of active VM templates suitable for client VMs / VM scale sets.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							Description: "Template key (used in `ccp_vm_instance.template`).",
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

func (d *vmTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vmTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	tpls, err := d.client.ListQemuTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read VM templates", err.Error())
		return
	}
	state := vmTemplatesModel{Templates: make([]vmTemplateModel, 0, len(tpls))}
	for _, t := range tpls {
		state.Templates = append(state.Templates, vmTemplateModel{
			Key:         types.StringValue(t.Key),
			DisplayName: types.StringValue(t.DisplayName),
			IsDefault:   types.BoolValue(t.IsDefault),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
