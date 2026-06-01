// Package acmednsproviders implements the ccp_acme_dns_providers data source —
// the catalog of DNS providers supported for Let's Encrypt DNS-01 challenges
// (load balancer and application gateway listeners), with the credential field
// names each provider expects.
package acmednsproviders

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*acmeDNSProvidersDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*acmeDNSProvidersDataSource)(nil)
)

func New() datasource.DataSource { return &acmeDNSProvidersDataSource{} }

type acmeDNSProvidersDataSource struct{ client *client.Client }

type model struct {
	Providers types.Map `tfsdk:"providers"`
}

// providerObjectType describes one catalog entry: a label and the list of
// credential field names the provider expects in acme_dns_credentials.
var providerObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"label":  types.StringType,
		"fields": types.ListType{ElemType: types.StringType},
	},
}

func (d *acmeDNSProvidersDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_acme_dns_providers"
}

func (d *acmeDNSProvidersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Catalog of DNS providers supported for Let's Encrypt DNS-01 challenges " +
			"(load balancer and application gateway listeners), with the credential field names each " +
			"provider expects.",
		Attributes: map[string]schema.Attribute{
			"providers": schema.MapNestedAttribute{
				MarkdownDescription: "Supported DNS providers keyed by provider id (e.g. `cloudflare`).",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"label": schema.StringAttribute{
							MarkdownDescription: "Human-readable provider name.",
							Computed:            true,
						},
						"fields": schema.ListAttribute{
							MarkdownDescription: "Credential field names expected in `acme_dns_credentials` for this provider.",
							ElementType:         types.StringType,
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *acmeDNSProvidersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *acmeDNSProvidersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	// The catalog is identical for load balancers and application gateways.
	catalog, err := d.client.ListLBAcmeDNSProviders(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read ACME DNS providers",
			"An error occurred while fetching the supported DNS provider catalog: "+err.Error(),
		)
		return
	}

	elems := make(map[string]attr.Value, len(catalog))
	for key, p := range catalog {
		fields, diags := types.ListValueFrom(ctx, types.StringType, p.Fields)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		obj, diags := types.ObjectValue(providerObjectType.AttrTypes, map[string]attr.Value{
			"label":  types.StringValue(p.Label),
			"fields": fields,
		})
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		elems[key] = obj
	}

	providers, diags := types.MapValue(providerObjectType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model{Providers: providers})...)
}
