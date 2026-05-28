// Package containerscaleset implements the ccp_container_scale_set data source.
package containerscaleset

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
	_ datasource.DataSource              = (*cssDS)(nil)
	_ datasource.DataSourceWithConfigure = (*cssDS)(nil)
)

func New() datasource.DataSource { return &cssDS{} }

type cssDS struct{ client *client.Client }

type cssDSModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Region           types.String `tfsdk:"region"`
	Plan             types.String `tfsdk:"plan"`
	Template         types.String `tfsdk:"template"`
	VnetID           types.String `tfsdk:"vnet_id"`
	MinInstances     types.Int64  `tfsdk:"min_instances"`
	MaxInstances     types.Int64  `tfsdk:"max_instances"`
	DesiredInstances types.Int64  `tfsdk:"desired_instances"`
	AutoRepair       types.Bool   `tfsdk:"auto_repair"`
	Status           types.String `tfsdk:"status"`
	ErrorMessage     types.String `tfsdk:"error_message"`
	Tags             types.List   `tfsdk:"tags"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func (d *cssDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_container_scale_set"
}

func (d *cssDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a container scale set by `id` or `(name, region)`.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true},
			"name":              schema.StringAttribute{Optional: true, Computed: true},
			"region":            schema.StringAttribute{Optional: true, Computed: true},
			"plan":              schema.StringAttribute{Computed: true},
			"template":          schema.StringAttribute{Computed: true},
			"vnet_id":           schema.StringAttribute{Computed: true},
			"min_instances":     schema.Int64Attribute{Computed: true},
			"max_instances":     schema.Int64Attribute{Computed: true},
			"desired_instances": schema.Int64Attribute{Computed: true},
			"auto_repair":       schema.BoolAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
			"error_message":     schema.StringAttribute{Computed: true},
			"tags":              schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":        schema.StringAttribute{Computed: true},
			"updated_at":        schema.StringAttribute{Computed: true},
		},
	}
}

func (d *cssDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *cssDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg cssDSModel
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

	var found *client.ContainerScaleSet
	if hasID {
		got, err := d.client.GetContainerScaleSet(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read container scale set", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListContainerScaleSets(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list container scale sets", err.Error())
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
			resp.Diagnostics.AddError("Container scale set not found", fmt.Sprintf("No scale set named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple container scale sets matched", fmt.Sprintf("Found %d in region %q.", len(matches), wantRegion))
			return
		}
	}

	state := cssDSModel{
		ID:               types.StringValue(found.ID),
		Name:             types.StringValue(found.Name),
		Region:           types.StringValue(found.Region),
		Plan:             types.StringValue(found.Plan),
		Template:         types.StringValue(found.Template),
		MinInstances:     types.Int64Value(int64(found.MinInstances)),
		MaxInstances:     types.Int64Value(int64(found.MaxInstances)),
		DesiredInstances: types.Int64Value(int64(found.DesiredInstances)),
		AutoRepair:       types.BoolValue(found.AutoRepair),
		Status:           types.StringValue(found.Status),
		CreatedAt:        types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt:        types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	if found.VnetID != nil {
		state.VnetID = types.StringValue(*found.VnetID)
	} else {
		state.VnetID = types.StringNull()
	}
	if found.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*found.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
