// Package dbvalkeyinstance implements the ccp_db_valkey_instance data source.
package dbvalkeyinstance

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*valkeyDS)(nil)
	_ datasource.DataSourceWithConfigure = (*valkeyDS)(nil)
)

func New() datasource.DataSource { return &valkeyDS{} }

type valkeyDS struct{ client *client.Client }

type valkeyDSModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	Engine          types.String `tfsdk:"engine"`
	EngineVersion   types.String `tfsdk:"engine_version"`
	Tier            types.String `tfsdk:"tier"`
	Plan            types.String `tfsdk:"plan"`
	VpcID           types.String `tfsdk:"vpc_id"`
	VnetID          types.String `tfsdk:"vnet_id"`
	Status          types.String `tfsdk:"status"`
	EndpointVnetIP  types.String `tfsdk:"endpoint_vnet_ip"`
	EndpointPort    types.Int64  `tfsdk:"endpoint_port"`
	Replicas        types.Int64  `tfsdk:"replicas"`
	StorageGB       types.Int64  `tfsdk:"storage_gb"`
	CPUMillicores   types.Int64  `tfsdk:"cpu_millicores"`
	MemoryMB        types.Int64  `tfsdk:"memory_mb"`
	ErrorMessage    types.String `tfsdk:"error_message"`
	Tags            types.List   `tfsdk:"tags"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
}

func (d *valkeyDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_db_valkey_instance"
}

func (d *valkeyDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a Valkey (Redis-compatible) instance by `id` or `name`. Credentials are NOT surfaced.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true},
			"name":              schema.StringAttribute{Optional: true, Computed: true},
			"region":            schema.StringAttribute{Computed: true},
			"engine":            schema.StringAttribute{Computed: true},
			"engine_version":    schema.StringAttribute{Computed: true},
			"tier":              schema.StringAttribute{Computed: true},
			"plan":              schema.StringAttribute{Computed: true},
			"vpc_id":            schema.StringAttribute{Computed: true},
			"vnet_id":           schema.StringAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
			"endpoint_vnet_ip":  schema.StringAttribute{Computed: true},
			"endpoint_port":     schema.Int64Attribute{Computed: true},
			"replicas":          schema.Int64Attribute{Computed: true},
			"storage_gb":        schema.Int64Attribute{Computed: true},
			"cpu_millicores":    schema.Int64Attribute{Computed: true},
			"memory_mb":         schema.Int64Attribute{Computed: true},
			"error_message":     schema.StringAttribute{Computed: true},
			"tags":              schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"public_ip_id":      schema.StringAttribute{Computed: true},
			"public_ip_address": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *valkeyDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *valkeyDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg valkeyDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	if (hasID && hasName) || (!hasID && !hasName) {
		resp.Diagnostics.AddError("Lookup arguments", "Provide exactly one of `id` or `name`.")
		return
	}

	var found *client.DbValkeyInstance
	if hasID {
		got, err := d.client.GetDbValkey(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read Valkey instance", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListDbValkey(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list Valkey instances", err.Error())
			return
		}
		want := cfg.Name.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == want {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("Valkey instance not found", fmt.Sprintf("No instance named %q.", want))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple Valkey instances matched", fmt.Sprintf("Found %d instances named %q.", len(matches), want))
			return
		}
	}

	state := valkeyDSModel{
		ID:            types.StringValue(found.ID),
		Name:          types.StringValue(found.Name),
		Region:        types.StringValue(found.Region),
		Engine:        types.StringValue(found.Engine),
		Tier:          types.StringValue(found.Tier),
		Plan:          types.StringValue(found.Plan),
		VpcID:         types.StringValue(found.VpcID),
		VnetID:        types.StringValue(found.VnetID),
		Status:        types.StringValue(found.Status),
		Replicas:      types.Int64Value(int64(found.Replicas)),
		StorageGB:     types.Int64Value(int64(found.StorageGB)),
		CPUMillicores: types.Int64Value(int64(found.CPUMillicores)),
		MemoryMB:      types.Int64Value(int64(found.MemoryMB)),
	}
	setStrPtr(&state.EngineVersion, found.EngineVersion)
	setStrPtr(&state.EndpointVnetIP, found.EndpointVnetIP)
	setStrPtr(&state.ErrorMessage, found.ErrorMessage)
	setStrPtr(&state.PublicIPID, found.PublicIPID)
	setStrPtr(&state.PublicIPAddress, found.PublicIPAddress)
	if found.EndpointPort != nil {
		state.EndpointPort = types.Int64Value(int64(*found.EndpointPort))
	} else {
		state.EndpointPort = types.Int64Null()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func setStrPtr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}
