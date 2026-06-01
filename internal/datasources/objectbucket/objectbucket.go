// Package objectbucket implements the ccp_object_bucket data source.
package objectbucket

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*bucketDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*bucketDataSource)(nil)
)

func New() datasource.DataSource { return &bucketDataSource{} }

type bucketDataSource struct{ client *client.Client }

type bucketDSModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Region       types.String `tfsdk:"region"`
	EndpointURL  types.String `tfsdk:"endpoint_url"`
	SizeBytes    types.Int64  `tfsdk:"size_bytes"`
	Status       types.String `tfsdk:"status"`
	IsPublic     types.Bool   `tfsdk:"is_public"`
	ErrorMessage types.String `tfsdk:"error_message"`
	Tags         types.List   `tfsdk:"tags"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func (d *bucketDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_object_bucket"
}

func (d *bucketDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an S3 Object Bucket by `id` or `(name, region)`. Credentials are not surfaced — use `ccp_object_storage_key`.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Optional: true, Computed: true},
			"name":          schema.StringAttribute{Optional: true, Computed: true},
			"region":        schema.StringAttribute{Optional: true, Computed: true},
			"endpoint_url":  schema.StringAttribute{Computed: true},
			"size_bytes":    schema.Int64Attribute{Computed: true},
			"status":        schema.StringAttribute{Computed: true},
			"is_public":     schema.BoolAttribute{Computed: true},
			"error_message": schema.StringAttribute{Computed: true},
			"tags":          schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":    schema.StringAttribute{Computed: true},
			"updated_at":    schema.StringAttribute{Computed: true},
		},
	}
}

func (d *bucketDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *bucketDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg bucketDSModel
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

	var found *client.ObjectBucket
	if hasID {
		got, err := d.client.GetObjectBucket(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read object bucket", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListObjectBuckets(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list object buckets", err.Error())
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
			resp.Diagnostics.AddError("Object bucket not found", fmt.Sprintf("No bucket named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple object buckets matched", fmt.Sprintf("Found %d buckets named %q in region %q.", len(matches), wantName, wantRegion))
			return
		}
	}

	state := bucketDSModel{
		ID:        types.StringValue(found.ID),
		Name:      types.StringValue(found.Name),
		Region:    types.StringValue(found.Region),
		SizeBytes: types.Int64Value(found.SizeBytes),
		Status:    types.StringValue(found.Status),
		IsPublic:  types.BoolValue(found.IsPublic),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt: types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	if found.EndpointURL != nil {
		state.EndpointURL = types.StringValue(*found.EndpointURL)
	} else {
		state.EndpointURL = types.StringNull()
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
