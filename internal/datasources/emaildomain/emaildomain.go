// Package emaildomain implements the ccp_email_domain data source — look up a
// hosted mail domain by `id` or by `name`.
//
// The reason to reach for it is `verification` and `dns_records`: the records
// to publish in the public zone of the domain, and the state observed there.
package emaildomain

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	rdomain "github.com/cetic-group/terraform-provider-ccp/internal/resources/emaildomain"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*emailDomainDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*emailDomainDataSource)(nil)
)

func New() datasource.DataSource { return &emailDomainDataSource{} }

type emailDomainDataSource struct{ client *client.Client }

type emailDomainDSModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Status            types.String `tfsdk:"status"`
	VerifiedAt        types.String `tfsdk:"verified_at"`
	DKIMGeneratedAt   types.String `tfsdk:"dkim_generated_at"`
	ExternallyManaged types.Bool   `tfsdk:"externally_managed"`
	AccountsCount     types.Int64  `tfsdk:"accounts_count"`
	AliasesCount      types.Int64  `tfsdk:"aliases_count"`
	CreatedAt         types.String `tfsdk:"created_at"`
	Verification      types.Object `tfsdk:"verification"`
	DNSRecords        types.List   `tfsdk:"dns_records"`
	ClientConfig      types.Object `tfsdk:"client_config"`
}

func (d *emailDomainDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_email_domain"
}

// dnsRecordAttributes is the shape of `verification` and of each `dns_records`
// entry. Same payload as the resource, same descriptions.
func dnsRecordAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"type":  schema.StringAttribute{Computed: true, MarkdownDescription: "Record type: `MX`, `TXT`, `CNAME` or `TLSA`."},
		"name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the record to publish."},
		"value": schema.StringAttribute{Computed: true, MarkdownDescription: "Exact value to publish, complete — a `MX` includes its priority."},
		"status": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "State observed in the public zone: `ok`, `missing`, `mismatch`, " +
				"`conflict` (several SPF records — merge them) or `over_lookup_limit` (the value is " +
				"right but the zone exceeds the SPF lookup budget).",
		},
		"hostname":             schema.StringAttribute{Computed: true, MarkdownDescription: "For a `MX`, the server alone. Paired with `priority`; null for other types."},
		"priority":             schema.Int64Attribute{Computed: true, MarkdownDescription: "For a `MX`, the priority. Paired with `hostname`."},
		"exceeds_lookup_limit": schema.BoolAttribute{Computed: true, MarkdownDescription: "The value shown is not publishable as-is: it would exceed the SPF lookup budget."},
		"purpose":              schema.StringAttribute{Computed: true, MarkdownDescription: "What the record is for, in one sentence."},
	}
}

func endpointAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"protocol": schema.StringAttribute{Computed: true, MarkdownDescription: "`imap`, `pop3` or `smtp`."},
		"hostname": schema.StringAttribute{Computed: true, MarkdownDescription: "Server name to enter in the mail client."},
		"port":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Port to connect to."},
		"security": schema.StringAttribute{Computed: true, MarkdownDescription: "`tls` or `starttls`."},
	}
}

func (d *emailDomainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a hosted mail domain by `id` **or** by `name` — exactly one " +
			"of the two.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the domain. Conflicts with `name`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Domain name, e.g. `example.com`. Conflicts with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"status":             schema.StringAttribute{Computed: true, MarkdownDescription: "`pending_verification`, `active` or `suspended`."},
			"verified_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "When ownership was established."},
			"dkim_generated_at":  schema.StringAttribute{Computed: true, MarkdownDescription: "When the signing key was created."},
			"externally_managed": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the console is read-only on this domain because it is driven from infrastructure code."},
			"accounts_count":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of mailboxes on the domain."},
			"aliases_count":      schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of aliases on the domain."},
			"created_at":         schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
			"verification": schema.SingleNestedAttribute{
				MarkdownDescription: "The `TXT` record proving you own the domain — the only one that blocks activation.",
				Computed:            true,
				Attributes:          dnsRecordAttributes(),
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "The `MX`, SPF, DKIM and DMARC records expected in the public zone, each with the state observed there.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: dnsRecordAttributes()},
			},
			"client_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Settings to copy into a mail application.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"incoming":      schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Incoming mail server.", Attributes: endpointAttributes()},
					"outgoing":      schema.SingleNestedAttribute{Computed: true, MarkdownDescription: "Outgoing mail server.", Attributes: endpointAttributes()},
					"username_hint": schema.StringAttribute{Computed: true, MarkdownDescription: "Reminder that the user name is the full address."},
				},
			},
		},
	}
}

func (d *emailDomainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *emailDomainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg emailDomainDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name`, not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name`.")
		return
	}

	id := cfg.ID.ValueString()
	if !hasID {
		// The list shape carries neither the expected records nor the
		// mail-client settings, so resolve the name and then read the domain.
		domains, err := d.client.ListEmailDomains(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list mail domains", err.Error())
			return
		}
		want := cfg.Name.ValueString()
		for i := range domains {
			if domains[i].Name == want {
				id = domains[i].ID
				break
			}
		}
		if id == "" {
			resp.Diagnostics.AddError("Mail domain not found",
				fmt.Sprintf("No mail domain named %q in this organisation.", want))
			return
		}
	}

	dom, err := d.client.GetEmailDomain(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read mail domain", err.Error())
		return
	}

	state, diags := dsStateFrom(dom)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func dsStateFrom(dom *client.EmailDomain) (emailDomainDSModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	m := emailDomainDSModel{
		ID:                types.StringValue(dom.ID),
		Name:              types.StringValue(dom.Name),
		Status:            types.StringValue(dom.Status),
		ExternallyManaged: types.BoolValue(dom.ExternallyManaged),
		CreatedAt:         types.StringValue(dom.CreatedAt),
	}
	if dom.VerifiedAt != nil {
		m.VerifiedAt = types.StringValue(*dom.VerifiedAt)
	} else {
		m.VerifiedAt = types.StringNull()
	}
	if dom.DKIMGeneratedAt != nil {
		m.DKIMGeneratedAt = types.StringValue(*dom.DKIMGeneratedAt)
	} else {
		m.DKIMGeneratedAt = types.StringNull()
	}
	if dom.AccountsCount != nil {
		m.AccountsCount = types.Int64Value(*dom.AccountsCount)
	} else {
		m.AccountsCount = types.Int64Null()
	}
	if dom.AliasesCount != nil {
		m.AliasesCount = types.Int64Value(*dom.AliasesCount)
	} else {
		m.AliasesCount = types.Int64Null()
	}

	if dom.Verification == nil {
		m.Verification = types.ObjectNull(rdomain.DNSRecordAttrTypes)
	} else {
		obj, d := rdomain.DNSRecordObject(*dom.Verification)
		diags.Append(d...)
		m.Verification = obj
	}

	recordType := types.ObjectType{AttrTypes: rdomain.DNSRecordAttrTypes}
	elems := make([]attr.Value, 0, len(dom.Records))
	for _, rec := range dom.Records {
		obj, d := rdomain.DNSRecordObject(rec)
		diags.Append(d...)
		elems = append(elems, obj)
	}
	list, d := types.ListValue(recordType, elems)
	diags.Append(d...)
	m.DNSRecords = list

	cfg, d := rdomain.ClientConfigObject(dom.ClientConfig)
	diags.Append(d...)
	m.ClientConfig = cfg

	return m, diags
}
