// Package customtemplate implements the ccp_custom_template data source.
package customtemplate

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*ctDS)(nil)
	_ datasource.DataSourceWithConfigure = (*ctDS)(nil)
)

func New() datasource.DataSource { return &ctDS{} }

type ctDS struct{ client *client.Client }

type ctDSModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	TemplateType       types.String `tfsdk:"template_type"`
	Region             types.String `tfsdk:"region"`
	Status             types.String `tfsdk:"status"`
	ErrorMessage       types.String `tfsdk:"error_message"`
	DiskGB             types.Int64  `tfsdk:"disk_gb"`
	SourceInstanceID   types.String `tfsdk:"source_instance_id"`
	SourceInstanceType types.String `tfsdk:"source_instance_type"`
	OsFamily           types.String `tfsdk:"os_family"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func (d *ctDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_custom_template"
}

func (d *ctDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a custom template (snapshot promoted to reusable image) by `id` or `name`.",
		Attributes: map[string]schema.Attribute{
			"id":                   schema.StringAttribute{Optional: true, Computed: true},
			"name":                 schema.StringAttribute{Optional: true, Computed: true},
			"description":          schema.StringAttribute{Computed: true},
			"template_type":        schema.StringAttribute{Computed: true},
			"region":               schema.StringAttribute{Computed: true},
			"status":               schema.StringAttribute{Computed: true},
			"error_message":        schema.StringAttribute{Computed: true},
			"disk_gb":              schema.Int64Attribute{Computed: true},
			"source_instance_id":   schema.StringAttribute{Computed: true},
			"source_instance_type": schema.StringAttribute{Computed: true},
			"os_family": schema.StringAttribute{
				MarkdownDescription: "Operating system family of the template (`linux` or " +
					"`windows`). A template captured from a Windows VM stays `windows`; " +
					"recreate a VM/VMSS from it with `windows_license_consent = true`.",
				Computed: true,
			},
			"created_at": schema.StringAttribute{Computed: true},
			"updated_at": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *ctDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ctDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg ctDSModel
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

	var found *client.CustomTemplate
	if hasID {
		got, err := d.client.GetCustomTemplate(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read custom template", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListCustomTemplates(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list custom templates", err.Error())
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
			resp.Diagnostics.AddError("Custom template not found", fmt.Sprintf("No template named %q.", want))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple custom templates matched", fmt.Sprintf("Found %d templates named %q.", len(matches), want))
			return
		}
	}

	state := ctDSModel{
		ID:           types.StringValue(found.ID),
		Name:         types.StringValue(found.Name),
		TemplateType: types.StringValue(found.TemplateType),
		Region:       types.StringValue(found.Region),
		Status:       types.StringValue(found.Status),
		CreatedAt:    types.StringValue(found.CreatedAt),
		UpdatedAt:    types.StringValue(found.UpdatedAt),
	}
	osFamily := found.OSFamily
	if osFamily == "" {
		osFamily = "linux"
	}
	state.OsFamily = types.StringValue(osFamily)
	setStrPtr(&state.Description, found.Description)
	setStrPtr(&state.ErrorMessage, found.ErrorMessage)
	setStrPtr(&state.SourceInstanceID, found.SourceInstanceID)
	setStrPtr(&state.SourceInstanceType, found.SourceInstanceType)
	if found.DiskGB != nil {
		state.DiskGB = types.Int64Value(int64(*found.DiskGB))
	} else {
		state.DiskGB = types.Int64Null()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func setStrPtr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}
