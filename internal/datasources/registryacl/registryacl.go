// Package registryacl implements the ccp_registry_acl data source.
package registryacl

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
	_ datasource.DataSource              = (*aclDS)(nil)
	_ datasource.DataSourceWithConfigure = (*aclDS)(nil)
)

func New() datasource.DataSource { return &aclDS{} }

type aclDS struct{ client *client.Client }

type aclDSModel struct {
	ID          types.String `tfsdk:"id"`
	RegistryID  types.String `tfsdk:"registry_id"`
	UserID      types.String `tfsdk:"user_id"`
	Username    types.String `tfsdk:"username"`
	RepoPattern types.String `tfsdk:"repo_pattern"`
	Actions     types.List   `tfsdk:"actions"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (d *aclDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_acl"
}

func (d *aclDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a registry ACL by `(id, registry_id)`.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Required: true},
			"registry_id":  schema.StringAttribute{Required: true},
			"user_id":      schema.StringAttribute{Computed: true},
			"username":     schema.StringAttribute{Computed: true},
			"repo_pattern": schema.StringAttribute{Computed: true},
			"actions":      schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":   schema.StringAttribute{Computed: true},
			"updated_at":   schema.StringAttribute{Computed: true},
		},
	}
}

func (d *aclDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *aclDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg aclDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	registryID := cfg.RegistryID.ValueString()
	wantID := cfg.ID.ValueString()

	list, err := d.client.ListRegistryACLs(ctx, registryID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list registry ACLs", err.Error())
		return
	}
	var found *client.RegistryACL
	for i := range list {
		if list[i].ID == wantID {
			found = &list[i]
			break
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Registry ACL not found",
			fmt.Sprintf("No ACL %q in registry %q.", wantID, registryID))
		return
	}
	state := aclDSModel{
		ID:          types.StringValue(found.ID),
		RegistryID:  types.StringValue(found.RegistryID),
		UserID:      types.StringValue(found.UserID),
		Username:    types.StringValue(found.Username),
		RepoPattern: types.StringValue(found.RepoPattern),
		CreatedAt:   types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt:   types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	actions, _ := types.ListValueFrom(ctx, types.StringType, found.Actions)
	state.Actions = actions
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
