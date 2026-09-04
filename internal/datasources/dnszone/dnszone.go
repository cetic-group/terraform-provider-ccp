// Package dnszone implements the ccp_dns_zone data source — look up a private
// DNS zone by `id` or by `name`.
//
// The main reason to reach for it is `resolver_endpoints`: the address to hand
// to a machine as its name server, and the subnet that address belongs to.
package dnszone

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*dnsZoneDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*dnsZoneDataSource)(nil)
)

func New() datasource.DataSource { return &dnsZoneDataSource{} }

type dnsZoneDataSource struct{ client *client.Client }

var endpointAttrTypes = map[string]attr.Type{
	"address":   types.StringType,
	"vnet_id":   types.StringType,
	"vnet_name": types.StringType,
	"vnet_cidr": types.StringType,
}

var challengeAttrTypes = map[string]attr.Type{
	"record_name":  types.StringType,
	"record_type":  types.StringType,
	"record_value": types.StringType,
}

type dnsZoneDSModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	VpcID           types.String `tfsdk:"vpc_id"`
	Region          types.String `tfsdk:"region"`
	Status          types.String `tfsdk:"status"`
	DefaultTTL      types.Int64  `tfsdk:"default_ttl"`
	DNSSECEnabled   types.Bool   `tfsdk:"dnssec_enabled"`
	ErrorMessage    types.String `tfsdk:"error_message"`
	RecordSetsCount types.Int64  `tfsdk:"record_sets_count"`
	CreatedAt       types.String `tfsdk:"created_at"`

	ResolverAddresses      types.List   `tfsdk:"resolver_addresses"`
	ResolverEndpoints      types.List   `tfsdk:"resolver_endpoints"`
	ResolverTier           types.String `tfsdk:"resolver_tier"`
	ResolverStatus         types.String `tfsdk:"resolver_status"`
	NsHostname             types.String `tfsdk:"ns_hostname"`
	AppliesToNewGuestsOnly types.Bool   `tfsdk:"applies_to_new_guests_only"`

	OwnershipChallenge types.Object `tfsdk:"ownership_challenge"`
}

func (d *dnsZoneDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_dns_zone"
}

func (d *dnsZoneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a private DNS zone by `id` **or** by `name` — exactly one of " +
			"the two. Zone names are unique within your organisation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the zone. Conflicts with `name`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the zone, e.g. `corp.internal`. Conflicts with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the private network the zone is served in.",
				Computed:            true,
			},
			"region":            schema.StringAttribute{Computed: true, MarkdownDescription: "Region the zone is served from."},
			"status":            schema.StringAttribute{Computed: true, MarkdownDescription: "`pending_verification`, `provisioning`, `active` or `error`."},
			"default_ttl":       schema.Int64Attribute{Computed: true, MarkdownDescription: "Default time-to-live of the zone, in seconds."},
			"dnssec_enabled":    schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the zone is signed."},
			"error_message":     schema.StringAttribute{Computed: true, MarkdownDescription: "Why the zone could not be brought up, when `status` says so."},
			"record_sets_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "Number of records you declared; the apex record the platform maintains is not counted."},
			"created_at":        schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
			"resolver_addresses": schema.ListAttribute{
				MarkdownDescription: "Addresses to use as name server from this network — one per " +
					"subnet served. From a machine, use the address of ITS OWN subnet.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"resolver_endpoints": schema.ListNestedAttribute{
				MarkdownDescription: "The same addresses, each with the subnet it serves.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"address":   schema.StringAttribute{Computed: true, MarkdownDescription: "Name server address to use from this subnet."},
						"vnet_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "UUID of the subnet."},
						"vnet_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the subnet. May be empty."},
						"vnet_cidr": schema.StringAttribute{Computed: true, MarkdownDescription: "CIDR of the subnet."},
					},
				},
			},
			"resolver_tier":              schema.StringAttribute{Computed: true, MarkdownDescription: "Service level serving the network: `dev` or `prod`."},
			"resolver_status":            schema.StringAttribute{Computed: true, MarkdownDescription: "State of the name server itself."},
			"ns_hostname":                schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the server published at the apex of the zone."},
			"applies_to_new_guests_only": schema.BoolAttribute{Computed: true, MarkdownDescription: "Machines receive the name server when they are created; existing ones keep theirs."},
			"ownership_challenge": schema.SingleNestedAttribute{
				MarkdownDescription: "Record to publish in the public DNS of the domain to prove " +
					"ownership. Null on an internal suffix and once the proof is accepted.",
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"record_name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the record to publish."},
					"record_type":  schema.StringAttribute{Computed: true, MarkdownDescription: "Always `TXT`."},
					"record_value": schema.StringAttribute{Computed: true, MarkdownDescription: "Exact value to publish."},
				},
			},
		},
	}
}

func (d *dnsZoneDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dnsZoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg dnsZoneDSModel
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
		// Resolve by name through the list, then read the zone itself: the
		// list shape carries no resolver block (CLAUDE.md pitfall #5), and the
		// resolver is the whole point of this data source.
		zones, err := d.client.ListDNSZones(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list DNS zones", err.Error())
			return
		}
		want := cfg.Name.ValueString()
		for i := range zones {
			if zones[i].Name == want {
				id = zones[i].ID
				break
			}
		}
		if id == "" {
			resp.Diagnostics.AddError("DNS zone not found",
				fmt.Sprintf("No DNS zone named %q in this organisation.", want))
			return
		}
	}

	zone, err := d.client.GetDNSZone(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read DNS zone", err.Error())
		return
	}

	state, diags := dsStateFrom(ctx, zone)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func dsStateFrom(ctx context.Context, z *client.DNSZone) (dnsZoneDSModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	m := dnsZoneDSModel{
		ID:            types.StringValue(z.ID),
		Name:          types.StringValue(z.Name),
		VpcID:         types.StringValue(z.VpcID),
		Region:        types.StringValue(z.Region),
		Status:        types.StringValue(z.Status),
		DefaultTTL:    types.Int64Value(z.DefaultTTL),
		DNSSECEnabled: types.BoolValue(z.DNSSECEnabled),
		CreatedAt:     types.StringValue(z.CreatedAt),
	}
	if z.ErrorMessage != nil {
		m.ErrorMessage = types.StringValue(*z.ErrorMessage)
	} else {
		m.ErrorMessage = types.StringNull()
	}
	if z.RecordSetsCount != nil {
		m.RecordSetsCount = types.Int64Value(*z.RecordSetsCount)
	} else {
		m.RecordSetsCount = types.Int64Null()
	}

	endpointsType := types.ObjectType{AttrTypes: endpointAttrTypes}
	if z.Resolver == nil {
		m.ResolverAddresses = types.ListNull(types.StringType)
		m.ResolverEndpoints = types.ListNull(endpointsType)
		m.ResolverTier = types.StringNull()
		m.ResolverStatus = types.StringNull()
		m.NsHostname = types.StringNull()
		m.AppliesToNewGuestsOnly = types.BoolNull()
	} else {
		addrs, d := types.ListValueFrom(ctx, types.StringType, z.Resolver.Addresses)
		diags.Append(d...)
		m.ResolverAddresses = addrs

		elems := make([]attr.Value, 0, len(z.Resolver.Endpoints))
		for _, e := range z.Resolver.Endpoints {
			cidr := types.StringNull()
			if e.VnetCIDR != nil {
				cidr = types.StringValue(*e.VnetCIDR)
			}
			obj, d := types.ObjectValue(endpointAttrTypes, map[string]attr.Value{
				"address":   types.StringValue(e.Address),
				"vnet_id":   types.StringValue(e.VnetID),
				"vnet_name": types.StringValue(e.VnetName),
				"vnet_cidr": cidr,
			})
			diags.Append(d...)
			elems = append(elems, obj)
		}
		list, d := types.ListValue(endpointsType, elems)
		diags.Append(d...)
		m.ResolverEndpoints = list

		m.ResolverTier = types.StringValue(z.Resolver.Tier)
		m.ResolverStatus = types.StringValue(z.Resolver.Status)
		m.NsHostname = types.StringValue(z.Resolver.NsHostname)
		m.AppliesToNewGuestsOnly = types.BoolValue(z.Resolver.AppliesToNewGuestsOnly)
	}

	if z.OwnershipChallenge == nil {
		m.OwnershipChallenge = types.ObjectNull(challengeAttrTypes)
	} else {
		obj, d := types.ObjectValue(challengeAttrTypes, map[string]attr.Value{
			"record_name":  types.StringValue(z.OwnershipChallenge.RecordName),
			"record_type":  types.StringValue(z.OwnershipChallenge.RecordType),
			"record_value": types.StringValue(z.OwnershipChallenge.RecordValue),
		})
		diags.Append(d...)
		m.OwnershipChallenge = obj
	}
	return m, diags
}
