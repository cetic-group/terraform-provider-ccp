// Package emaildomains implements the ccp_email_domains data source — every
// hosted mail domain of the organisation.
//
// The list shape of the API carries no expected DNS records and no
// mail-client settings; use the `ccp_email_domain` data source for one domain
// when those are needed.
package emaildomains

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*emailDomainsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*emailDomainsDataSource)(nil)
)

func New() datasource.DataSource { return &emailDomainsDataSource{} }

type emailDomainsDataSource struct{ client *client.Client }

type domainModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Status            types.String `tfsdk:"status"`
	VerifiedAt        types.String `tfsdk:"verified_at"`
	DKIMGeneratedAt   types.String `tfsdk:"dkim_generated_at"`
	ExternallyManaged types.Bool   `tfsdk:"externally_managed"`
	AccountsCount     types.Int64  `tfsdk:"accounts_count"`
	AliasesCount      types.Int64  `tfsdk:"aliases_count"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

type emailDomainsDSModel struct {
	Domains []domainModel `tfsdk:"domains"`
}

func (d *emailDomainsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_email_domains"
}

func (d *emailDomainsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every hosted mail domain of the organisation.",
		Attributes: map[string]schema.Attribute{
			"domains": schema.ListNestedAttribute{
				MarkdownDescription: "The mail domains.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                 schema.StringAttribute{Computed: true, MarkdownDescription: "UUID of the domain."},
						"name":               schema.StringAttribute{Computed: true, MarkdownDescription: "Domain name."},
						"status":             schema.StringAttribute{Computed: true, MarkdownDescription: "`pending_verification`, `active` or `suspended`."},
						"verified_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "When ownership was established."},
						"dkim_generated_at":  schema.StringAttribute{Computed: true, MarkdownDescription: "When the signing key was created."},
						"externally_managed": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the console is read-only on this domain."},
						"accounts_count":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of mailboxes."},
						"aliases_count":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of aliases."},
						"created_at":         schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
					},
				},
			},
		},
	}
}

func (d *emailDomainsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *emailDomainsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	domains, err := d.client.ListEmailDomains(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list mail domains", err.Error())
		return
	}
	var state emailDomainsDSModel
	for i := range domains {
		dom := domains[i]
		m := domainModel{
			ID:                types.StringValue(dom.ID),
			Name:              types.StringValue(dom.Name),
			Status:            types.StringValue(dom.Status),
			ExternallyManaged: types.BoolValue(dom.ExternallyManaged),
			CreatedAt:         types.StringValue(dom.CreatedAt),
			VerifiedAt:        types.StringNull(),
			DKIMGeneratedAt:   types.StringNull(),
			AccountsCount:     types.Int64Null(),
			AliasesCount:      types.Int64Null(),
		}
		if dom.VerifiedAt != nil {
			m.VerifiedAt = types.StringValue(*dom.VerifiedAt)
		}
		if dom.DKIMGeneratedAt != nil {
			m.DKIMGeneratedAt = types.StringValue(*dom.DKIMGeneratedAt)
		}
		if dom.AccountsCount != nil {
			m.AccountsCount = types.Int64Value(*dom.AccountsCount)
		}
		if dom.AliasesCount != nil {
			m.AliasesCount = types.Int64Value(*dom.AliasesCount)
		}
		state.Domains = append(state.Domains, m)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
