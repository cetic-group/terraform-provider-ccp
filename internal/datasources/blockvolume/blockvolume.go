// Package blockvolume implements the ccp_block_volume data source.
package blockvolume

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
	_ datasource.DataSource              = (*bvDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*bvDataSource)(nil)
)

func New() datasource.DataSource { return &bvDataSource{} }

type bvDataSource struct{ client *client.Client }

type bvDSModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Region         types.String `tfsdk:"region"`
	SizeGB         types.Int64  `tfsdk:"size_gb"`
	Status         types.String `tfsdk:"status"`
	AttachedToID   types.String `tfsdk:"attached_to_id"`
	AttachedToType types.String `tfsdk:"attached_to_type"`
	AttachedToName types.String `tfsdk:"attached_to_name"`
	RBDPool        types.String `tfsdk:"rbd_pool"`
	RBDImage       types.String `tfsdk:"rbd_image"`
	ErrorMessage   types.String `tfsdk:"error_message"`
	Tags           types.List   `tfsdk:"tags"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (d *bvDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_volume"
}

func (d *bvDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a Block Volume (Ceph RBD) by `id` or `(name, region)`.",
		Attributes: map[string]schema.Attribute{
			"id":               schema.StringAttribute{Optional: true, Computed: true},
			"name":             schema.StringAttribute{Optional: true, Computed: true},
			"region":           schema.StringAttribute{Optional: true, Computed: true},
			"size_gb":          schema.Int64Attribute{Computed: true},
			"status":           schema.StringAttribute{Computed: true},
			"attached_to_id":   schema.StringAttribute{Computed: true},
			"attached_to_type": schema.StringAttribute{Computed: true},
			"attached_to_name": schema.StringAttribute{Computed: true},
			"rbd_pool":         schema.StringAttribute{Computed: true},
			"rbd_image":        schema.StringAttribute{Computed: true},
			"error_message":    schema.StringAttribute{Computed: true},
			"tags":             schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":       schema.StringAttribute{Computed: true},
			"updated_at":       schema.StringAttribute{Computed: true},
		},
	}
}

func (d *bvDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *bvDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg bvDSModel
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

	var found *client.BlockVolume
	if hasID {
		got, err := d.client.GetBlockVolume(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read block volume", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListBlockVolumes(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list block volumes", err.Error())
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
			resp.Diagnostics.AddError("Block volume not found", fmt.Sprintf("No volume named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple block volumes matched", fmt.Sprintf("Found %d volumes named %q in region %q.", len(matches), wantName, wantRegion))
			return
		}
	}

	state := bvDSModel{
		ID:        types.StringValue(found.ID),
		Name:      types.StringValue(found.Name),
		Region:    types.StringValue(found.Region),
		SizeGB:    types.Int64Value(int64(found.SizeGB)),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt: types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	setStrPtr(&state.AttachedToID, found.AttachedToID)
	setStrPtr(&state.AttachedToType, found.AttachedToType)
	setStrPtr(&state.AttachedToName, found.AttachedToName)
	setStrPtr(&state.RBDPool, found.RBDPool)
	setStrPtr(&state.RBDImage, found.RBDImage)
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
