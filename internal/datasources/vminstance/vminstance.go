// Package vminstance implements the ccp_vm_instance data source.
package vminstance

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*vmInstanceDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*vmInstanceDataSource)(nil)
)

func New() datasource.DataSource { return &vmInstanceDataSource{} }

type vmInstanceDataSource struct{ client *client.Client }

type vmInstanceDSModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	Plan            types.String `tfsdk:"plan"`
	Cores           types.Int64  `tfsdk:"cores"`
	MemoryMB        types.Int64  `tfsdk:"memory_mb"`
	DiskGB          types.Int64  `tfsdk:"disk_gb"`
	Template        types.String `tfsdk:"template"`
	Status          types.String `tfsdk:"status"`
	IPAddress       types.String `tfsdk:"ip_address"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
	VnetID          types.String `tfsdk:"vnet_id"`
	ScaleSetID      types.String `tfsdk:"scale_set_id"`
	UserData        types.String `tfsdk:"user_data"`
	ErrorMessage    types.String `tfsdk:"error_message"`
	HasRootPassword types.Bool   `tfsdk:"has_root_password"`
	Tags            types.List   `tfsdk:"tags"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func (d *vmInstanceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vm_instance"
}

func (d *vmInstanceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a VM instance by `id`, or by `(name, region)`.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true},
			"name":              schema.StringAttribute{Optional: true, Computed: true},
			"region":            schema.StringAttribute{Optional: true, Computed: true},
			"plan":              schema.StringAttribute{Computed: true},
			"cores":             schema.Int64Attribute{Computed: true},
			"memory_mb":         schema.Int64Attribute{Computed: true},
			"disk_gb":           schema.Int64Attribute{Computed: true},
			"template":          schema.StringAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
			"ip_address":        schema.StringAttribute{Computed: true},
			"public_ip_address": schema.StringAttribute{Computed: true},
			"vnet_id":           schema.StringAttribute{Computed: true},
			"scale_set_id":      schema.StringAttribute{Computed: true},
			"user_data":         schema.StringAttribute{Computed: true},
			"error_message":     schema.StringAttribute{Computed: true},
			"has_root_password": schema.BoolAttribute{Computed: true},
			"tags":              schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":        schema.StringAttribute{Computed: true},
			"updated_at":        schema.StringAttribute{Computed: true},
		},
	}
}

func (d *vmInstanceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *vmInstanceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg vmInstanceDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	hasRegion := !cfg.Region.IsNull() && !cfg.Region.IsUnknown() && cfg.Region.ValueString() != ""

	switch {
	case hasID && (hasName || hasRegion):
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id`, or both `name` and `region`.")
		return
	case !hasID && !(hasName && hasRegion):
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id`, or both `name` and `region`.")
		return
	}

	var found *client.VMInstance
	if hasID {
		got, err := d.client.GetVMInstance(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read VM instance", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListVMInstances(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list VM instances", err.Error())
			return
		}
		wantName, wantRegion := cfg.Name.ValueString(), cfg.Region.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == wantName && list[i].Region == wantRegion {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("VM instance not found", fmt.Sprintf("No VM named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple VM instances matched", fmt.Sprintf("Found %d VMs named %q in region %q.", len(matches), wantName, wantRegion))
			return
		}
	}

	state := vmInstanceDSModel{
		ID:              types.StringValue(found.ID),
		Name:            types.StringValue(found.Name),
		Region:          types.StringValue(found.Region),
		Plan:            types.StringValue(found.Plan),
		Cores:           types.Int64Value(int64(found.Cores)),
		MemoryMB:        types.Int64Value(int64(found.MemoryMB)),
		DiskGB:          types.Int64Value(int64(found.DiskGB)),
		Template:        types.StringValue(found.Template),
		Status:          types.StringValue(found.Status),
		HasRootPassword: types.BoolValue(found.HasRootPassword),
		CreatedAt:       types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt:       types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	setStrPtr(&state.IPAddress, found.IPAddress)
	setStrPtr(&state.PublicIPAddress, found.PublicIPAddress)
	setStrPtr(&state.VnetID, found.VnetID)
	setStrPtr(&state.ScaleSetID, found.ScaleSetID)
	setStrPtr(&state.UserData, found.UserData)
	setStrPtr(&state.ErrorMessage, found.ErrorMessage)
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
