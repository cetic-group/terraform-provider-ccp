// Package vmsnapshot implements the ccp_vm_snapshot data source.
package vmsnapshot

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*snapDS)(nil)
	_ datasource.DataSourceWithConfigure = (*snapDS)(nil)
)

func New() datasource.DataSource { return &snapDS{} }

type snapDS struct{ client *client.Client }

type snapDSModel struct {
	ID           types.String `tfsdk:"id"`
	VmInstanceID types.String `tfsdk:"vm_instance_id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Status       types.String `tfsdk:"status"`
	SizeBytes    types.Int64  `tfsdk:"size_bytes"`
	ErrorMessage types.String `tfsdk:"error_message"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (d *snapDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_snapshot"
}

func (d *snapDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a VM snapshot by `(id, vm_instance_id)` or `(name, vm_instance_id)`.",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Optional: true, Computed: true},
			"vm_instance_id": schema.StringAttribute{Required: true},
			"name":           schema.StringAttribute{Optional: true, Computed: true},
			"description":    schema.StringAttribute{Computed: true},
			"status":         schema.StringAttribute{Computed: true},
			"size_bytes":     schema.Int64Attribute{Computed: true},
			"error_message":  schema.StringAttribute{Computed: true},
			"created_at":     schema.StringAttribute{Computed: true},
		},
	}
}

func (d *snapDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *snapDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg snapDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vmID := cfg.VmInstanceID.ValueString()
	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name` (with `vm_instance_id`).")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name` (with `vm_instance_id`).")
		return
	}

	var found *client.VmSnapshot
	if hasID {
		got, err := d.client.GetVmSnapshot(ctx, vmID, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read VM snapshot", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListVmSnapshots(ctx, vmID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list VM snapshots", err.Error())
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
			resp.Diagnostics.AddError("VM snapshot not found", fmt.Sprintf("No snapshot named %q on VM %q.", want, vmID))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple VM snapshots matched", fmt.Sprintf("Found %d on VM %q.", len(matches), vmID))
			return
		}
	}

	state := snapDSModel{
		ID:           types.StringValue(found.ID),
		VmInstanceID: types.StringValue(found.VmInstanceID),
		Name:         types.StringValue(found.Name),
		Status:       types.StringValue(found.Status),
		CreatedAt:    types.StringValue(found.CreatedAt),
	}
	if found.Description != nil {
		state.Description = types.StringValue(*found.Description)
	} else {
		state.Description = types.StringNull()
	}
	if found.ErrorMsg != nil {
		state.ErrorMessage = types.StringValue(*found.ErrorMsg)
	} else {
		state.ErrorMessage = types.StringNull()
	}
	if found.SizeBytes != nil {
		state.SizeBytes = types.Int64Value(*found.SizeBytes)
	} else {
		state.SizeBytes = types.Int64Null()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
