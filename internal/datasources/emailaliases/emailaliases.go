// Package emailaliases implements the ccp_email_aliases data source — the mail
// aliases of the organisation, optionally restricted to one domain.
package emailaliases

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*emailAliasesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*emailAliasesDataSource)(nil)
)

func New() datasource.DataSource { return &emailAliasesDataSource{} }

type emailAliasesDataSource struct{ client *client.Client }

type aliasModel struct {
	ID           types.String `tfsdk:"id"`
	Address      types.String `tfsdk:"address"`
	Destinations types.List   `tfsdk:"destinations"`
	Wildcard     types.Bool   `tfsdk:"wildcard"`
	Comment      types.String `tfsdk:"comment"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

type emailAliasesDSModel struct {
	DomainID types.String `tfsdk:"domain_id"`
	Aliases  []aliasModel `tfsdk:"aliases"`
}

func (d *emailAliasesDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_email_aliases"
}

func (d *emailAliasesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The mail aliases of the organisation, optionally restricted to one domain.",
		Attributes: map[string]schema.Attribute{
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "Restrict the list to this domain (`ccp_email_domain.id`). " +
					"Omit for every alias of the organisation.",
				Optional: true,
			},
			"aliases": schema.ListNestedAttribute{
				MarkdownDescription: "The aliases.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":           schema.StringAttribute{Computed: true, MarkdownDescription: "UUID of the alias."},
						"address":      schema.StringAttribute{Computed: true, MarkdownDescription: "Source address."},
						"destinations": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Addresses mail is delivered to."},
						"wildcard":     schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the alias is a catch-all."},
						"comment":      schema.StringAttribute{Computed: true, MarkdownDescription: "Free-form note."},
						"created_at":   schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
					},
				},
			},
		},
	}
}

func (d *emailAliasesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *emailAliasesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg emailAliasesDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := ""
	if !cfg.DomainID.IsNull() && !cfg.DomainID.IsUnknown() {
		domainID = cfg.DomainID.ValueString()
	}

	aliases, err := d.client.ListEmailAliases(ctx, domainID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list mail aliases", err.Error())
		return
	}

	state := emailAliasesDSModel{DomainID: cfg.DomainID}
	for i := range aliases {
		a := aliases[i]
		dests, diags := types.ListValueFrom(ctx, types.StringType, a.Destinations)
		resp.Diagnostics.Append(diags...)
		m := aliasModel{
			ID:           types.StringValue(a.ID),
			Address:      types.StringValue(a.Address),
			Destinations: dests,
			Wildcard:     types.BoolValue(a.Wildcard),
			Comment:      types.StringNull(),
			CreatedAt:    types.StringValue(a.CreatedAt),
		}
		if a.Comment != nil {
			m.Comment = types.StringValue(*a.Comment)
		}
		state.Aliases = append(state.Aliases, m)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
