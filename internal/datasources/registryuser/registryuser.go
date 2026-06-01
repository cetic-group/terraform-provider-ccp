// Package registryuser implements the ccp_registry_user data source.
package registryuser

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
	_ datasource.DataSource              = (*userDS)(nil)
	_ datasource.DataSourceWithConfigure = (*userDS)(nil)
)

func New() datasource.DataSource { return &userDS{} }

type userDS struct{ client *client.Client }

type userDSModel struct {
	ID         types.String `tfsdk:"id"`
	RegistryID types.String `tfsdk:"registry_id"`
	Username   types.String `tfsdk:"username"`
	IsAdmin    types.Bool   `tfsdk:"is_admin"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func (d *userDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_registry_user"
}

func (d *userDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a registry user by `(username, registry_id)`. The password is NEVER exposed.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true},
			"registry_id": schema.StringAttribute{Required: true},
			"username":    schema.StringAttribute{Required: true},
			"is_admin":    schema.BoolAttribute{Computed: true},
			"created_at":  schema.StringAttribute{Computed: true},
		},
	}
}

func (d *userDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *userDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg userDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	registryID := cfg.RegistryID.ValueString()
	wantUser := cfg.Username.ValueString()

	list, err := d.client.ListRegistryUsers(ctx, registryID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list registry users", err.Error())
		return
	}
	var found *client.RegistryUser
	for i := range list {
		if list[i].Username == wantUser {
			found = &list[i]
			break
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Registry user not found",
			fmt.Sprintf("No user %q in registry %q.", wantUser, registryID))
		return
	}
	state := userDSModel{
		ID:         types.StringValue(found.ID),
		RegistryID: types.StringValue(found.RegistryID),
		Username:   types.StringValue(found.Username),
		IsAdmin:    types.BoolValue(found.IsAdmin),
		CreatedAt:  types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
