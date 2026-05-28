// Package secret implements the ccp_secret data source (Secret Manager v1).
//
// Looks up a secret by `id` or by `name` (exclusive — exactly one must be
// provided). Returns metadata only — `data` (plaintext values) is NEVER
// exposed by a data source on the platform. To consume plaintext from
// Terraform, use the resource (`ccp_secret`) and reference its `data`
// attribute, or fetch via the CLI / API outside of Terraform.
package secret

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*secretDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*secretDataSource)(nil)
)

// New returns the data source factory used by `provider.DataSources()`.
func New() datasource.DataSource { return &secretDataSource{} }

type secretDataSource struct{ client *client.Client }

type secretDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Description   types.String `tfsdk:"description"`
	Version       types.Int64  `tfsdk:"version"`
	Tags          types.List   `tfsdk:"tags"`
	LastRotatedAt types.String `tfsdk:"last_rotated_at"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func (d *secretDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_secret"
}

func (d *secretDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a CETIC Cloud secret by `id` or by `name`. Exactly one of `id` or " +
			"`name` must be provided.\n\n" +
			"~> **Returns metadata only.** The plaintext `data` is never exposed by a data source on the " +
			"platform — calling the reveal endpoint is audit-logged and reserved for explicit operator " +
			"workflows (`cetic secret value <id>` in the CLI, or the resource `ccp_secret` whose state " +
			"holds the plaintext from its most recent Create / Update).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the secret. Conflicts with `name`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "DNS-friendly secret name. Conflicts with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Free-form description.",
				Computed:            true,
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "Server-side monotonic version counter.",
				Computed:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form tags attached to the secret.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"last_rotated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the most recent rotation, or null.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last metadata or rotation update.",
				Computed:            true,
			},
		},
	}
}

func (d *secretDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *secretDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg secretDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments",
			"Provide either `id` or `name`, not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments",
			"Provide either `id` or `name` to look up a secret.")
		return
	}

	var found *client.Secret
	if hasID {
		got, err := d.client.GetSecret(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read secret", err.Error())
			return
		}
		found = got
	} else {
		got, err := d.client.GetSecretByName(ctx, cfg.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up secret", err.Error())
			return
		}
		found = got
	}

	state := secretDataSourceModel{
		ID:        types.StringValue(found.ID),
		Name:      types.StringValue(found.Name),
		Version:   types.Int64Value(found.Version),
		CreatedAt: types.StringValue(found.CreatedAt),
		UpdatedAt: types.StringValue(found.UpdatedAt),
	}
	if found.Description != nil {
		state.Description = types.StringValue(*found.Description)
	} else {
		state.Description = types.StringNull()
	}
	tagValues := make([]string, 0, len(found.Tags))
	tagValues = append(tagValues, found.Tags...)
	tagsList, tagsDiags := types.ListValueFrom(ctx, types.StringType, tagValues)
	resp.Diagnostics.Append(tagsDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Tags = tagsList
	if found.LastRotatedAt != nil {
		state.LastRotatedAt = types.StringValue(*found.LastRotatedAt)
	} else {
		state.LastRotatedAt = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
