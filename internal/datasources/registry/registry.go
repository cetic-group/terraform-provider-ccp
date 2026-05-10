// Package registry implements the ccp_registry data source.
//
// Looks up a CETIC Container Registry by `id`, or by `(name, region)`.
package registry

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
	_ datasource.DataSource              = (*registryDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*registryDataSource)(nil)
)

func New() datasource.DataSource { return &registryDataSource{} }

type registryDataSource struct{ client *client.Client }

type registryDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Slug           types.String `tfsdk:"slug"`
	Region         types.String `tfsdk:"region"`
	ExposePublic   types.Bool   `tfsdk:"expose_public"`
	ExposePrivate  types.Bool   `tfsdk:"expose_private"`
	URL            types.String `tfsdk:"url"`
	ImageTag       types.String `tfsdk:"image_tag"`
	GCScheduleCron types.String `tfsdk:"gc_schedule_cron"`
	Status         types.String `tfsdk:"status"`
	StorageUsedGB  types.Int64  `tfsdk:"storage_used_gb"`
	LastPushAt     types.String `tfsdk:"last_push_at"`
	AdminUsername  types.String `tfsdk:"admin_username"`
	Tags           types.List   `tfsdk:"tags"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (d *registryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry"
}

func (d *registryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a CETIC Container Registry by `id` or by `(name, region)`. " +
			"Exactly one of those discriminators must be provided.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the registry to look up. Conflicts with `name` + `region`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the registry to look up. Combined with `region` to identify it.",
				Optional:            true,
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region of the registry to look up. Required when looking up by `name`.",
				Optional:            true,
				Computed:            true,
			},
			"slug":             schema.StringAttribute{Computed: true},
			"expose_public":    schema.BoolAttribute{Computed: true},
			"expose_private":   schema.BoolAttribute{Computed: true},
			"url":              schema.StringAttribute{Computed: true},
			"image_tag":        schema.StringAttribute{Computed: true},
			"gc_schedule_cron": schema.StringAttribute{Computed: true},
			"status":           schema.StringAttribute{Computed: true},
			"storage_used_gb":  schema.Int64Attribute{Computed: true},
			"last_push_at":     schema.StringAttribute{Computed: true},
			"admin_username":   schema.StringAttribute{Computed: true},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"created_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *registryDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *registryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg registryDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	hasRegion := !cfg.Region.IsNull() && !cfg.Region.IsUnknown() && cfg.Region.ValueString() != ""

	switch {
	case hasID && (hasName || hasRegion):
		resp.Diagnostics.AddError("Conflicting lookup arguments",
			"Provide either `id`, or both `name` and `region` — not both.")
		return
	case !hasID && !(hasName && hasRegion):
		resp.Diagnostics.AddError("Missing lookup arguments",
			"Provide either `id`, or both `name` and `region`.")
		return
	}

	var found *client.Registry
	if hasID {
		got, err := d.client.GetRegistry(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read registry", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListRegistries(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list registries", err.Error())
			return
		}
		wantName, wantRegion := cfg.Name.ValueString(), cfg.Region.ValueString()
		for i := range list {
			if list[i].Name == wantName && list[i].Region == wantRegion {
				found = &list[i]
				break
			}
		}
		if found == nil {
			resp.Diagnostics.AddError("Registry not found",
				fmt.Sprintf("No registry named %q in region %q.", wantName, wantRegion))
			return
		}
	}

	state := registryDataSourceModel{
		ID:             types.StringValue(found.ID),
		Name:           types.StringValue(found.Name),
		Slug:           types.StringValue(found.Slug),
		Region:         types.StringValue(found.Region),
		ExposePublic:   types.BoolValue(found.ExposePublic),
		ExposePrivate:  types.BoolValue(found.ExposePrivate),
		ImageTag:       types.StringValue(found.ImageTag),
		GCScheduleCron: types.StringValue(found.GCScheduleCron),
		Status:         types.StringValue(found.Status),
		CreatedAt:      types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	if found.URL != nil {
		state.URL = types.StringValue(*found.URL)
	} else {
		state.URL = types.StringNull()
	}
	if found.StorageUsedGB != nil {
		state.StorageUsedGB = types.Int64Value(*found.StorageUsedGB)
	} else {
		state.StorageUsedGB = types.Int64Null()
	}
	if found.LastPushAt != nil {
		state.LastPushAt = types.StringValue(*found.LastPushAt)
	} else {
		state.LastPushAt = types.StringNull()
	}
	if found.AdminUsername != nil {
		state.AdminUsername = types.StringValue(*found.AdminUsername)
	} else {
		state.AdminUsername = types.StringNull()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
