package organizations

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure interface compliance.
var (
	_ datasource.DataSource              = &organizationsDataSource{}
	_ datasource.DataSourceWithConfigure = &organizationsDataSource{}
)

// New returns a new instance of the ccp_organizations data source.
func New() datasource.DataSource {
	return &organizationsDataSource{}
}

// organizationsDataSource is the data source implementation.
type organizationsDataSource struct {
	client *client.Client
}

// organizationsDataSourceModel maps the data source schema state.
type organizationsDataSourceModel struct {
	Organizations []organizationModel `tfsdk:"organizations"`
}

// organizationModel maps a single organization object in state.
type organizationModel struct {
	ID                     types.String `tfsdk:"id"`
	OwnerTenantID          types.String `tfsdk:"owner_tenant_id"`
	Name                   types.String `tfsdk:"name"`
	Description            types.String `tfsdk:"description"`
	IsDefault              types.Bool   `tfsdk:"is_default"`
	Tags                   types.List   `tfsdk:"tags"`
	DefaultPaymentMethodID types.String `tfsdk:"default_payment_method_id"`
	HasPaymentMethod       types.Bool   `tfsdk:"has_payment_method"`
	HasSubscription        types.Bool   `tfsdk:"has_subscription"`
	CreatedAt              types.String `tfsdk:"created_at"`
	UpdatedAt              types.String `tfsdk:"updated_at"`
}

// Metadata returns the data source type name.
func (d *organizationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organizations"
}

// Schema defines the schema for the data source.
func (d *organizationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists organizations accessible to the current API key's tenant. " +
			"The active organization for resources is determined server-side by `api_keys.org_id` — " +
			"to target a different org, use a different API key via Terraform provider aliases.",
		Attributes: map[string]schema.Attribute{
			"organizations": schema.ListNestedAttribute{
				Description: "List of organizations accessible to the current API key's tenant.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Organization identifier (UUID).",
							Computed:    true,
						},
						"owner_tenant_id": schema.StringAttribute{
							Description: "Identifier of the tenant that owns this organization.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Human-readable organization name.",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "Optional description of the organization.",
							Computed:    true,
						},
						"is_default": schema.BoolAttribute{
							Description: "Whether this organization is the default one for its owner tenant.",
							Computed:    true,
						},
						"tags": schema.ListAttribute{
							Description: "Free-form tags attached to the organization.",
							Computed:    true,
							ElementType: types.StringType,
						},
						"default_payment_method_id": schema.StringAttribute{
							Description: "Identifier of the default payment method attached to this organization, if any.",
							Computed:    true,
						},
						"has_payment_method": schema.BoolAttribute{
							Description: "Whether the organization has at least one payment method attached.",
							Computed:    true,
						},
						"has_subscription": schema.BoolAttribute{
							Description: "Whether the organization has an active subscription.",
							Computed:    true,
						},
						"created_at": schema.StringAttribute{
							Description: "RFC3339 timestamp at which the organization was created.",
							Computed:    true,
						},
						"updated_at": schema.StringAttribute{
							Description: "RFC3339 timestamp of the last update to the organization.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

// Configure adds the provider-configured client to the data source.
func (d *organizationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// Read fetches the list of organizations from the CETIC Cloud API.
func (d *organizationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	orgs, err := d.client.ListOrganizations(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Read CETIC Cloud Organizations",
			"An error occurred while fetching the list of organizations: "+err.Error(),
		)
		return
	}

	state := organizationsDataSourceModel{
		Organizations: make([]organizationModel, 0, len(orgs)),
	}

	for _, o := range orgs {
		tagsValue, tagsDiags := types.ListValueFrom(ctx, types.StringType, o.Tags)
		resp.Diagnostics.Append(tagsDiags...)
		if resp.Diagnostics.HasError() {
			return
		}

		description := types.StringNull()
		if o.Description != nil {
			description = types.StringValue(*o.Description)
		}

		defaultPaymentMethodID := types.StringNull()
		if o.DefaultPaymentMethodID != nil {
			defaultPaymentMethodID = types.StringValue(*o.DefaultPaymentMethodID)
		}

		state.Organizations = append(state.Organizations, organizationModel{
			ID:                     types.StringValue(o.ID),
			OwnerTenantID:          types.StringValue(o.OwnerTenantID),
			Name:                   types.StringValue(o.Name),
			Description:            description,
			IsDefault:              types.BoolValue(o.IsDefault),
			Tags:                   tagsValue,
			DefaultPaymentMethodID: defaultPaymentMethodID,
			HasPaymentMethod:       types.BoolValue(o.HasPaymentMethod),
			HasSubscription:        types.BoolValue(o.HasSubscription),
			CreatedAt:              types.StringValue(o.CreatedAt.Format("2006-01-02T15:04:05Z07:00")),
			UpdatedAt:              types.StringValue(o.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")),
		})
	}

	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
