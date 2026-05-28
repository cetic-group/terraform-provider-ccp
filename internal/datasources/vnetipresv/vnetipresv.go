// Package vnetipresv implements the ccp_vnet_ip_reservation data source.
package vnetipresv

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*resDS)(nil)
	_ datasource.DataSourceWithConfigure = (*resDS)(nil)
)

func New() datasource.DataSource { return &resDS{} }

type resDS struct{ client *client.Client }

type resDSModel struct {
	ID          types.String `tfsdk:"id"`
	VnetID      types.String `tfsdk:"vnet_id"`
	Name        types.String `tfsdk:"name"`
	IP          types.String `tfsdk:"ip"`
	RangeEnd    types.String `tfsdk:"range_end"`
	Description types.String `tfsdk:"description"`
	Count       types.Int64  `tfsdk:"count_total"`
	Kind        types.String `tfsdk:"kind"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (d *resDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_vnet_ip_reservation"
}

func (d *resDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a VNet IP reservation by `(id, vnet_id)` or `(name, vnet_id)`. `count_total` mirrors the API's `count` field; the Terraform attribute is renamed because `count` is a reserved Terraform keyword.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true},
			"vnet_id":     schema.StringAttribute{Required: true},
			"name":        schema.StringAttribute{Optional: true, Computed: true},
			"ip":          schema.StringAttribute{Computed: true},
			"range_end":   schema.StringAttribute{Computed: true},
			"description": schema.StringAttribute{Computed: true},
			"count_total": schema.Int64Attribute{Computed: true},
			"kind":        schema.StringAttribute{Computed: true},
			"created_at":  schema.StringAttribute{Computed: true},
		},
	}
}

func (d *resDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *resDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg resDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vnetID := cfg.VnetID.ValueString()
	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name` (with `vnet_id`), not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name` (with `vnet_id`).")
		return
	}

	var found *client.VnetIpReservation
	if hasID {
		got, err := d.client.GetVnetIpReservation(ctx, vnetID, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read IP reservation", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListVnetIpReservations(ctx, vnetID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list IP reservations", err.Error())
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
			resp.Diagnostics.AddError("IP reservation not found", fmt.Sprintf("No reservation named %q in vnet %q.", want, vnetID))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple IP reservations matched", fmt.Sprintf("Found %d in vnet %q.", len(matches), vnetID))
			return
		}
	}

	state := resDSModel{
		ID:        types.StringValue(found.ID),
		VnetID:    types.StringValue(found.VnetID),
		Name:      types.StringValue(found.Name),
		IP:        types.StringValue(found.IP),
		Count:     types.Int64Value(int64(found.Count)),
		Kind:      types.StringValue(found.Kind),
		CreatedAt: types.StringValue(found.CreatedAt),
	}
	if found.RangeEnd != nil {
		state.RangeEnd = types.StringValue(*found.RangeEnd)
	} else {
		state.RangeEnd = types.StringNull()
	}
	if found.Description != nil {
		state.Description = types.StringValue(*found.Description)
	} else {
		state.Description = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
