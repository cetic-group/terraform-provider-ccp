// Package serviceaccount implements the ccp_service_account data source.
// Token is NEVER exposed by the datasource — only at create time on the resource.
package serviceaccount

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
	_ datasource.DataSource              = (*saDS)(nil)
	_ datasource.DataSourceWithConfigure = (*saDS)(nil)
)

func New() datasource.DataSource { return &saDS{} }

type saDS struct{ client *client.Client }

type saDSModel struct {
	ID          types.String `tfsdk:"id"`
	TenantID    types.String `tfsdk:"tenant_id"`
	OrgID       types.String `tfsdk:"org_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	TokenPrefix types.String `tfsdk:"token_prefix"`
	LastUsedAt  types.String `tfsdk:"last_used_at"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
	RotatedAt   types.String `tfsdk:"rotated_at"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (d *saDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (d *saDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a service account by `id` or `name`. The token is NEVER exposed — it is only returned at creation time on the resource.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Optional: true, Computed: true},
			"name":         schema.StringAttribute{Optional: true, Computed: true},
			"tenant_id":    schema.StringAttribute{Computed: true},
			"org_id":       schema.StringAttribute{Computed: true},
			"description":  schema.StringAttribute{Computed: true},
			"token_prefix": schema.StringAttribute{Computed: true},
			"last_used_at": schema.StringAttribute{Computed: true},
			"expires_at":   schema.StringAttribute{Computed: true},
			"rotated_at":   schema.StringAttribute{Computed: true},
			"created_at":   schema.StringAttribute{Computed: true},
		},
	}
}

func (d *saDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *saDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg saDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	if (hasID && hasName) || (!hasID && !hasName) {
		resp.Diagnostics.AddError("Lookup arguments", "Provide exactly one of `id` or `name`.")
		return
	}

	var found *client.ServiceAccount
	if hasID {
		got, err := d.client.GetServiceAccount(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read service account", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListServiceAccounts(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list service accounts", err.Error())
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
			resp.Diagnostics.AddError("Service account not found", fmt.Sprintf("No service account named %q.", want))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple service accounts matched", fmt.Sprintf("Found %d service accounts named %q.", len(matches), want))
			return
		}
	}

	state := saDSModel{
		ID:          types.StringValue(found.ID),
		TenantID:    types.StringValue(found.TenantID),
		OrgID:       types.StringValue(found.OrgID),
		Name:        types.StringValue(found.Name),
		TokenPrefix: types.StringValue(found.TokenPrefix),
		CreatedAt:   types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	if found.Description != nil {
		state.Description = types.StringValue(*found.Description)
	} else {
		state.Description = types.StringNull()
	}
	setTimePtr(&state.LastUsedAt, found.LastUsedAt)
	setTimePtr(&state.ExpiresAt, found.ExpiresAt)
	setTimePtr(&state.RotatedAt, found.RotatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func setTimePtr(dst *types.String, src *time.Time) {
	if src != nil {
		*dst = types.StringValue(src.Format(time.RFC3339))
	} else {
		*dst = types.StringNull()
	}
}
