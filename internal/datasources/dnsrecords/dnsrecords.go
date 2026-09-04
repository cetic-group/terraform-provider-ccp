// Package dnsrecords implements the ccp_dns_records data source — every record
// of a private DNS zone, including the ones the platform maintains.
package dnsrecords

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*dnsRecordsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsRecordsDataSource)(nil)
)

func New() datasource.DataSource { return &dnsRecordsDataSource{} }

type dnsRecordsDataSource struct{ client *client.Client }

type recordModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Type            types.String `tfsdk:"type"`
	TTL             types.Int64  `tfsdk:"ttl"`
	Records         types.Set    `tfsdk:"records"`
	IsSystemManaged types.Bool   `tfsdk:"is_system_managed"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

type dnsRecordsDSModel struct {
	ZoneID  types.String  `tfsdk:"zone_id"`
	Records []recordModel `tfsdk:"records"`
}

func (d *dnsRecordsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_dns_records"
}

func (d *dnsRecordsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every record of a private DNS zone. Records maintained by the " +
			"platform are listed too, flagged with `is_system_managed` — they are read only.",
		Attributes: map[string]schema.Attribute{
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the zone (`ccp_dns_zone.id`).",
				Required:            true,
			},
			"records": schema.ListNestedAttribute{
				MarkdownDescription: "The records of the zone.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.StringAttribute{Computed: true, MarkdownDescription: "UUID of the record."},
						"name": schema.StringAttribute{Computed: true, MarkdownDescription: "Fully qualified name of the record."},
						"type": schema.StringAttribute{Computed: true, MarkdownDescription: "Record type."},
						"ttl":  schema.Int64Attribute{Computed: true, MarkdownDescription: "How long the answer may be cached, in seconds."},
						"records": schema.SetAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Values answered for this name and type.",
						},
						"is_system_managed": schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the platform maintains this record."},
						"created_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
					},
				},
			},
		},
	}
}

func (d *dnsRecordsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dnsRecordsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg dnsRecordsDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sets, err := d.client.ListDNSRecordSets(ctx, cfg.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to list DNS records", err.Error())
		return
	}

	state := dnsRecordsDSModel{ZoneID: cfg.ZoneID}
	for i := range sets {
		values, diags := types.SetValueFrom(ctx, types.StringType, sets[i].Records)
		resp.Diagnostics.Append(diags...)
		state.Records = append(state.Records, recordModel{
			ID:              types.StringValue(sets[i].ID),
			Name:            types.StringValue(sets[i].Name),
			Type:            types.StringValue(sets[i].RecordType),
			TTL:             types.Int64Value(sets[i].TTL),
			Records:         values,
			IsSystemManaged: types.BoolValue(sets[i].IsSystemManaged),
			CreatedAt:       types.StringValue(sets[i].CreatedAt),
		})
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
