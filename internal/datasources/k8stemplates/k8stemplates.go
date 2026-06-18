package k8stemplates

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &k8sTemplatesDataSource{}
	_ datasource.DataSourceWithConfigure = &k8sTemplatesDataSource{}
)

func New() datasource.DataSource { return &k8sTemplatesDataSource{} }

type k8sTemplatesDataSource struct {
	client *client.Client
}

type k8sTemplatesModel struct {
	Templates []k8sTemplateModel `tfsdk:"templates"`
}

type k8sTemplateModel struct {
	OsKey       types.String `tfsdk:"os_key"`
	OsLabel     types.String `tfsdk:"os_label"`
	Os          types.String `tfsdk:"os"`
	DisplayName types.String `tfsdk:"display_name"`
	K8sVersion  types.String `tfsdk:"k8s_version"`
	Region      types.String `tfsdk:"region"`
	VMID        types.Int64  `tfsdk:"vmid"`
	BuiltAt     types.String `tfsdk:"built_at"`
}

func (d *k8sTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_k8s_templates"
}

func (d *k8sTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists Kubernetes node OS templates available on CETIC Cloud (admin-managed catalog). Used by `ccp_k8s_node_pool.os_key` to pick a base OS image.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				Description: "List of K8s node templates.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"os_key": schema.StringAttribute{
							Description: "OS key (used in `ccp_k8s_node_pool.os_key`). Example: `ccks-capi-flatcar-k1346`.",
							Computed:    true,
						},
						"os_label": schema.StringAttribute{
							Description: "Human-readable OS label.",
							Computed:    true,
						},
						"os": schema.StringAttribute{
							Description: "Node OS family slug for this template. One of `flatcar`, `ubuntu`, `rocky9`.",
							Computed:    true,
						},
						"display_name": schema.StringAttribute{
							Description: "Display name shown in the console.",
							Computed:    true,
						},
						"k8s_version": schema.StringAttribute{
							Description: "Kubernetes version baked into the template.",
							Computed:    true,
						},
						"region": schema.StringAttribute{
							Description: "Region where this template is built.",
							Computed:    true,
						},
						"vmid": schema.Int64Attribute{
							Description: "Proxmox VMID of the template (admin-internal, may be null).",
							Computed:    true,
						},
						"built_at": schema.StringAttribute{
							Description: "Timestamp at which the template was last built.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *k8sTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *k8sTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	tpls, err := d.client.ListK8sTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read K8s templates", err.Error())
		return
	}
	state := k8sTemplatesModel{Templates: make([]k8sTemplateModel, 0, len(tpls))}
	for _, t := range tpls {
		vmid := types.Int64Null()
		if t.VMID != nil {
			vmid = types.Int64Value(int64(*t.VMID))
		}
		built := types.StringNull()
		if t.BuiltAt != nil {
			built = types.StringValue(*t.BuiltAt)
		}
		state.Templates = append(state.Templates, k8sTemplateModel{
			OsKey:       types.StringValue(t.OsKey),
			OsLabel:     types.StringValue(t.OsLabel),
			Os:          types.StringValue(t.Os),
			DisplayName: types.StringValue(t.DisplayName),
			K8sVersion:  types.StringValue(t.K8sVersion),
			Region:      types.StringValue(t.Region),
			VMID:        vmid,
			BuiltAt:     built,
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
