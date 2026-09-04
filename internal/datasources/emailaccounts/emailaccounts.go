// Package emailaccounts implements the ccp_email_accounts data source — the
// mailboxes of the organisation, optionally restricted to one domain.
//
// No password appears here, in any shape: a mailbox password is reset, never
// read back.
package emailaccounts

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*emailAccountsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*emailAccountsDataSource)(nil)
)

func New() datasource.DataSource { return &emailAccountsDataSource{} }

type emailAccountsDataSource struct{ client *client.Client }

type accountModel struct {
	ID                 types.String `tfsdk:"id"`
	Address            types.String `tfsdk:"address"`
	QuotaBytes         types.Int64  `tfsdk:"quota_bytes"`
	UsageBytes         types.Int64  `tfsdk:"usage_bytes"`
	UsageUpdatedAt     types.String `tfsdk:"usage_updated_at"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	EnableIMAP         types.Bool   `tfsdk:"enable_imap"`
	EnablePOP          types.Bool   `tfsdk:"enable_pop"`
	IsSystemManaged    types.Bool   `tfsdk:"is_system_managed"`
	SendAsAnyAddress   types.Bool   `tfsdk:"send_as_any_address"`
	SendAsPending      types.Bool   `tfsdk:"send_as_pending"`
	ForwardEnabled     types.Bool   `tfsdk:"forward_enabled"`
	ForwardDestination types.List   `tfsdk:"forward_destination"`
	ForwardKeep        types.Bool   `tfsdk:"forward_keep"`
	Comment            types.String `tfsdk:"comment"`
	DisplayedName      types.String `tfsdk:"displayed_name"`
	CreatedAt          types.String `tfsdk:"created_at"`
}

type emailAccountsDSModel struct {
	DomainID types.String   `tfsdk:"domain_id"`
	Accounts []accountModel `tfsdk:"accounts"`
}

func (d *emailAccountsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_email_accounts"
}

func (d *emailAccountsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The mailboxes of the organisation, optionally restricted to one " +
			"domain. Mailbox passwords never appear here.",
		Attributes: map[string]schema.Attribute{
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "Restrict the list to this domain (`ccp_email_domain.id`). " +
					"Omit for every mailbox of the organisation.",
				Optional: true,
			},
			"accounts": schema.ListNestedAttribute{
				MarkdownDescription: "The mailboxes.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":               schema.StringAttribute{Computed: true, MarkdownDescription: "UUID of the mailbox."},
						"address":          schema.StringAttribute{Computed: true, MarkdownDescription: "Full address."},
						"quota_bytes":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Space reserved, in bytes — the billing basis."},
						"usage_bytes":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Last reading of the space occupied. Null while no reading has succeeded."},
						"usage_updated_at": schema.StringAttribute{Computed: true, MarkdownDescription: "When `usage_bytes` was last read."},
						"enabled":          schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the mailbox accepts logins and deliveries."},
						"enable_imap":      schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether IMAP is available."},
						"enable_pop":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether POP3 is available."},
						"is_system_managed": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether the platform owns this mailbox — the address collecting DMARC reports is one.",
						},
						"send_as_any_address": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the mailbox may send under any address of its domain."},
						"send_as_pending":     schema.BoolAttribute{Computed: true, MarkdownDescription: "`true` when that privilege is recorded but not yet applied."},
						"forward_enabled":     schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether incoming mail is forwarded."},
						"forward_destination": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Addresses mail is forwarded to."},
						"forward_keep":        schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether forwarded mail is also kept in the mailbox."},
						"comment":             schema.StringAttribute{Computed: true, MarkdownDescription: "Free-form note."},
						"displayed_name":      schema.StringAttribute{Computed: true, MarkdownDescription: "Name shown as the sender."},
						"created_at":          schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
					},
				},
			},
		},
	}
}

func (d *emailAccountsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *emailAccountsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg emailAccountsDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domainID := ""
	if !cfg.DomainID.IsNull() && !cfg.DomainID.IsUnknown() {
		domainID = cfg.DomainID.ValueString()
	}

	accounts, err := d.client.ListEmailAccounts(ctx, domainID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list mailboxes", err.Error())
		return
	}

	state := emailAccountsDSModel{DomainID: cfg.DomainID}
	for i := range accounts {
		a := accounts[i]
		forwards, diags := types.ListValueFrom(ctx, types.StringType, a.ForwardDestination)
		resp.Diagnostics.Append(diags...)
		m := accountModel{
			ID:                 types.StringValue(a.ID),
			Address:            types.StringValue(a.Address),
			QuotaBytes:         types.Int64Value(a.QuotaBytes),
			UsageBytes:         types.Int64Null(),
			UsageUpdatedAt:     types.StringNull(),
			Enabled:            types.BoolValue(a.Enabled),
			EnableIMAP:         types.BoolValue(a.EnableIMAP),
			EnablePOP:          types.BoolValue(a.EnablePOP),
			IsSystemManaged:    types.BoolValue(a.IsSystemManaged),
			SendAsAnyAddress:   types.BoolValue(a.SendAsAnyAddress),
			SendAsPending:      types.BoolValue(a.SendAsPending),
			ForwardEnabled:     types.BoolValue(a.ForwardEnabled),
			ForwardDestination: forwards,
			ForwardKeep:        types.BoolValue(a.ForwardKeep),
			Comment:            types.StringNull(),
			DisplayedName:      types.StringNull(),
			CreatedAt:          types.StringValue(a.CreatedAt),
		}
		if a.UsageBytes != nil {
			m.UsageBytes = types.Int64Value(*a.UsageBytes)
		}
		if a.UsageUpdatedAt != nil {
			m.UsageUpdatedAt = types.StringValue(*a.UsageUpdatedAt)
		}
		if a.Comment != nil {
			m.Comment = types.StringValue(*a.Comment)
		}
		if a.DisplayedName != nil {
			m.DisplayedName = types.StringValue(*a.DisplayedName)
		}
		state.Accounts = append(state.Accounts, m)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
