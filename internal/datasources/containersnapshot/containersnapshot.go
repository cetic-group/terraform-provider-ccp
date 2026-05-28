// Package containersnapshot implements the ccp_container_snapshot data source.
package containersnapshot

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
	ContainerID  types.String `tfsdk:"container_id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Status       types.String `tfsdk:"status"`
	SizeBytes    types.Int64  `tfsdk:"size_bytes"`
	ErrorMessage types.String `tfsdk:"error_message"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (d *snapDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_container_snapshot"
}

func (d *snapDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a container snapshot by `(id, container_id)` or `(name, container_id)`.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Optional: true, Computed: true},
			"container_id":  schema.StringAttribute{Required: true},
			"name":          schema.StringAttribute{Optional: true, Computed: true},
			"description":   schema.StringAttribute{Computed: true},
			"status":        schema.StringAttribute{Computed: true},
			"size_bytes":    schema.Int64Attribute{Computed: true},
			"error_message": schema.StringAttribute{Computed: true},
			"created_at":    schema.StringAttribute{Computed: true},
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
	cID := cfg.ContainerID.ValueString()
	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name` (with `container_id`).")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name` (with `container_id`).")
		return
	}

	var found *client.ContainerSnapshot
	if hasID {
		got, err := d.client.GetContainerSnapshot(ctx, cID, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read container snapshot", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListContainerSnapshots(ctx, cID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list container snapshots", err.Error())
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
			resp.Diagnostics.AddError("Container snapshot not found", fmt.Sprintf("No snapshot named %q on container %q.", want, cID))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple container snapshots matched", fmt.Sprintf("Found %d on container %q.", len(matches), cID))
			return
		}
	}

	state := snapDSModel{
		ID:          types.StringValue(found.ID),
		ContainerID: types.StringValue(found.ContainerID),
		Name:        types.StringValue(found.Name),
		Status:      types.StringValue(found.Status),
		CreatedAt:   types.StringValue(found.CreatedAt),
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
