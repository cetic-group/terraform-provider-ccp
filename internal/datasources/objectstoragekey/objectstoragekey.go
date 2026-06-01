// Package objectstoragekey implements the ccp_object_storage_key data source.
// The secret access key is NEVER exposed — only returned at create time on the resource.
package objectstoragekey

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*oskDS)(nil)
	_ datasource.DataSourceWithConfigure = (*oskDS)(nil)
)

func New() datasource.DataSource { return &oskDS{} }

type oskDS struct{ client *client.Client }

type oskDSModel struct {
	ID              types.String `tfsdk:"id"`
	Region          types.String `tfsdk:"region"`
	Label           types.String `tfsdk:"label"`
	AccessLevel     types.String `tfsdk:"access_level"`
	AccessKeyPrefix types.String `tfsdk:"access_key_prefix"`
	CreatedAt       types.String `tfsdk:"created_at"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	RevokedAt       types.String `tfsdk:"revoked_at"`
}

func (d *oskDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_object_storage_key"
}

func (d *oskDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an Object Storage subuser key by `id`. The secret is NEVER exposed (only returned once at create).",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Required: true},
			"region":            schema.StringAttribute{Computed: true},
			"label":             schema.StringAttribute{Computed: true},
			"access_level":      schema.StringAttribute{Computed: true},
			"access_key_prefix": schema.StringAttribute{Computed: true},
			"created_at":        schema.StringAttribute{Computed: true},
			"expires_at":        schema.StringAttribute{Computed: true},
			"revoked_at":        schema.StringAttribute{Computed: true},
		},
	}
}

func (d *oskDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *oskDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg oskDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := d.client.GetObjectStorageKey(ctx, cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read object storage key", err.Error())
		return
	}
	state := oskDSModel{
		ID:              types.StringValue(got.ID),
		Region:          types.StringValue(got.Region),
		Label:           types.StringValue(got.Label),
		AccessLevel:     types.StringValue(got.AccessLevel),
		AccessKeyPrefix: types.StringValue(got.AccessKeyPrefix),
		CreatedAt:       types.StringValue(got.CreatedAt),
	}
	if got.ExpiresAt != nil {
		state.ExpiresAt = types.StringValue(*got.ExpiresAt)
	} else {
		state.ExpiresAt = types.StringNull()
	}
	if got.RevokedAt != nil {
		state.RevokedAt = types.StringValue(*got.RevokedAt)
	} else {
		state.RevokedAt = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
