// Package apikey implements the ccp_api_key data source.
// The secret token is NEVER exposed — only at create time on the resource.
package apikey

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
	_ datasource.DataSource              = (*akDS)(nil)
	_ datasource.DataSourceWithConfigure = (*akDS)(nil)
)

func New() datasource.DataSource { return &akDS{} }

type akDS struct{ client *client.Client }

type akDSModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Prefix     types.String `tfsdk:"prefix"`
	Scopes     types.List   `tfsdk:"scopes"`
	ExpiresAt  types.String `tfsdk:"expires_at"`
	LastUsedAt types.String `tfsdk:"last_used_at"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (d *akDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_api_key"
}

func (d *akDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an API key by `id`. The bearer token is NEVER exposed (only returned once at create).",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Required: true},
			"name":         schema.StringAttribute{Computed: true},
			"prefix":       schema.StringAttribute{Computed: true},
			"scopes":       schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"expires_at":   schema.StringAttribute{Computed: true},
			"last_used_at": schema.StringAttribute{Computed: true},
			"created_at":   schema.StringAttribute{Computed: true},
		},
	}
}

func (d *akDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *akDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg akDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := d.client.GetApiKey(ctx, cfg.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read API key", err.Error())
		return
	}
	state := akDSModel{
		ID:        types.StringValue(got.ID),
		Name:      types.StringValue(got.Name),
		Prefix:    types.StringValue(got.Prefix),
		CreatedAt: types.StringValue(got.CreatedAt.Format(time.RFC3339)),
	}
	if got.ExpiresAt != nil {
		state.ExpiresAt = types.StringValue(*got.ExpiresAt)
	} else {
		state.ExpiresAt = types.StringNull()
	}
	if got.LastUsedAt != nil {
		state.LastUsedAt = types.StringValue(*got.LastUsedAt)
	} else {
		state.LastUsedAt = types.StringNull()
	}
	scopes, _ := types.ListValueFrom(ctx, types.StringType, got.Scopes)
	state.Scopes = scopes

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
