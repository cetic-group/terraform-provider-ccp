// Package sshkey implements the ccp_ssh_key data source.
package sshkey

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
	_ datasource.DataSource              = (*sshDS)(nil)
	_ datasource.DataSourceWithConfigure = (*sshDS)(nil)
)

func New() datasource.DataSource { return &sshDS{} }

type sshDS struct{ client *client.Client }

type sshDSModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Fingerprint       types.String `tfsdk:"fingerprint"`
	Scope             types.String `tfsdk:"scope"`
	CreatedByTenantID types.String `tfsdk:"created_by_tenant_id"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

func (d *sshDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_ssh_key"
}

func (d *sshDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an SSH key by `id` or `name`.",
		Attributes: map[string]schema.Attribute{
			"id":                   schema.StringAttribute{Optional: true, Computed: true},
			"name":                 schema.StringAttribute{Optional: true, Computed: true},
			"fingerprint":          schema.StringAttribute{Computed: true},
			"scope":                schema.StringAttribute{Computed: true},
			"created_by_tenant_id": schema.StringAttribute{Computed: true},
			"created_at":           schema.StringAttribute{Computed: true},
		},
	}
}

func (d *sshDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sshDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sshDSModel
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

	list, err := d.client.ListSSHKeys(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list SSH keys", err.Error())
		return
	}

	var found *client.SSHKey
	if hasID {
		want := cfg.ID.ValueString()
		for i := range list {
			if list[i].ID == want {
				found = &list[i]
				break
			}
		}
	} else {
		want := cfg.Name.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == want {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple SSH keys matched", fmt.Sprintf("Found %d SSH keys named %q.", len(matches), want))
			return
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("SSH key not found", "No matching SSH key.")
		return
	}

	state := sshDSModel{
		ID:                types.StringValue(found.ID),
		Name:              types.StringValue(found.Name),
		Fingerprint:       types.StringValue(found.Fingerprint),
		Scope:             types.StringValue(found.Scope),
		CreatedByTenantID: types.StringValue(found.CreatedByTenantID),
		CreatedAt:         types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
