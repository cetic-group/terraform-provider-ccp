// Package iamrole implements the ccp_iam_role data source.
//
// Looks up an IAM role by `id`, or by `(name, built_in)`. The 10 platform
// built-in roles (AdminAll, ReadOnlyAll, Member, RegistryAdmin,
// RegistryReader, BucketReader, BucketWriter, K8sViewer, BillingReader,
// NetworkAdmin) are exposed read-only via this data source — they are
// seeded server-side and have no tenant_id, so the only stable lookup
// key is `(name, built_in=true)`.
package iamrole

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
	_ datasource.DataSource              = (*iamRoleDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*iamRoleDataSource)(nil)
)

// New returns the data source factory used by `provider.DataSources()`.
func New() datasource.DataSource { return &iamRoleDataSource{} }

type iamRoleDataSource struct{ client *client.Client }

type iamRoleDataSourceModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	BuiltIn            types.Bool   `tfsdk:"built_in"`
	Description        types.String `tfsdk:"description"`
	PolicyDocumentJSON types.String `tfsdk:"policy_document_json"`
	PolicyHash         types.String `tfsdk:"policy_hash"`
	IsBuiltIn          types.Bool   `tfsdk:"is_built_in"`
	CreatedAt          types.String `tfsdk:"created_at"`
}

func (d *iamRoleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_iam_role"
}

func (d *iamRoleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a CETIC Cloud IAM role by `id`, or by `name` (optionally narrowed by " +
			"`built_in`). Exactly one of `id` or `name` must be set.\n\n" +
			"The 10 platform built-in roles (`AdminAll`, `ReadOnlyAll`, `Member`, `RegistryAdmin`, " +
			"`RegistryReader`, `BucketReader`, `BucketWriter`, `K8sViewer`, `BillingReader`, " +
			"`NetworkAdmin`) are stable across releases and identifiable by `(name, built_in = true)`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the role to look up. Conflicts with `name`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the role to look up. Conflicts with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"built_in": schema.BoolAttribute{
				MarkdownDescription: "When looking up by `name`, restrict the search to built-in roles " +
					"(`true`) or to custom roles (`false`). Leave unset to match either.",
				Optional: true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Free-form role description.",
				Computed:            true,
			},
			"policy_document_json": schema.StringAttribute{
				MarkdownDescription: "JCS-canonicalised PolicyDocument as a JSON string.",
				Computed:            true,
			},
			"policy_hash": schema.StringAttribute{
				MarkdownDescription: "SHA-256 hex of the canonical PolicyDocument.",
				Computed:            true,
			},
			"is_built_in": schema.BoolAttribute{
				MarkdownDescription: "Whether this role is platform-managed (built-in) or tenant-managed " +
					"(custom).",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
			},
		},
	}
}

func (d *iamRoleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *iamRoleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg iamRoleDataSourceModel
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
			"Provide either `id` or `name` to look up an IAM role.")
		return
	}

	var found *client.Role
	if hasID {
		got, err := d.client.GetRole(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read IAM role", err.Error())
			return
		}
		found = got
	} else {
		var builtInPtr *bool
		if !cfg.BuiltIn.IsNull() && !cfg.BuiltIn.IsUnknown() {
			v := cfg.BuiltIn.ValueBool()
			builtInPtr = &v
		}
		got, err := d.client.GetRoleByName(ctx, cfg.Name.ValueString(), builtInPtr)
		if err != nil {
			resp.Diagnostics.AddError("Failed to look up IAM role", err.Error())
			return
		}
		found = got
	}

	state := iamRoleDataSourceModel{
		ID:                 types.StringValue(found.ID),
		Name:               types.StringValue(found.Name),
		BuiltIn:            cfg.BuiltIn,
		PolicyDocumentJSON: types.StringValue(string(found.PolicyDocument)),
		PolicyHash:         types.StringValue(found.PolicyHash),
		IsBuiltIn:          types.BoolValue(found.IsBuiltIn),
		CreatedAt:          types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	if found.Description != nil {
		state.Description = types.StringValue(*found.Description)
	} else {
		state.Description = types.StringNull()
	}
	// Mirror back built_in input if the caller did not provide it.
	if state.BuiltIn.IsNull() || state.BuiltIn.IsUnknown() {
		state.BuiltIn = types.BoolValue(found.IsBuiltIn)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
