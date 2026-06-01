// Package customtemplate implements the ccp_custom_template Terraform resource.
//
// Custom templates are tenant-owned reusable images, created from a snapshot of
// either an existing container or VM. Exactly one of `source_container_id` /
// `source_vm_id` must be set at create time (forces replacement on change —
// changing the source effectively means re-creating the template).
//
// Creation is asynchronous on the API side (HTTP 202) but the resource Read
// returns immediately with the current `status`. To wait for `ready` use
// `terraform output` polling or a `null_resource` with `local-exec` retry.
package customtemplate

import (
	"context"
	"fmt"
	"regexp"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*customTemplateResource)(nil)
	_ resource.ResourceWithConfigure   = (*customTemplateResource)(nil)
	_ resource.ResourceWithImportState = (*customTemplateResource)(nil)
)

func New() resource.Resource { return &customTemplateResource{} }

type customTemplateResource struct {
	client *client.Client
}

type customTemplateModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	SourceContainerID  types.String `tfsdk:"source_container_id"`
	SourceVMID         types.String `tfsdk:"source_vm_id"`
	TemplateType       types.String `tfsdk:"template_type"`
	Region             types.String `tfsdk:"region"`
	Status             types.String `tfsdk:"status"`
	ErrorMessage       types.String `tfsdk:"error_message"`
	DiskGB             types.Int64  `tfsdk:"disk_gb"`
	SourceInstanceID   types.String `tfsdk:"source_instance_id"`
	SourceInstanceType types.String `tfsdk:"source_instance_type"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_\- ]{2,100}$`)

func (r *customTemplateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_custom_template"
}

func (r *customTemplateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a custom template — a reusable snapshot of an existing container or VM " +
			"that can be referenced as a base image for new instances. Exactly one of " +
			"`source_container_id` or `source_vm_id` must be set at creation. Creation is " +
			"asynchronous: the resource returns with the initial `status` (typically `creating`); " +
			"poll `ccp_custom_template.status` for `ready` before relying on the template.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the custom template.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (2-100 chars; alphanumerics, `_`, `-`, spaces).",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(2, 100),
					stringvalidator.RegexMatches(nameRe, "name must match ^[a-zA-Z0-9_\\- ]{2,100}$"),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Optional description (max 500 chars).",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtMost(500)},
			},
			"source_container_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the source container to snapshot. Mutually exclusive with `source_vm_id`. Forces replacement on change.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("source_vm_id")),
				},
			},
			"source_vm_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the source VM to snapshot. Mutually exclusive with `source_container_id`. Forces replacement on change.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators: []validator.String{
					stringvalidator.ConflictsWith(path.MatchRoot("source_container_id")),
				},
			},
			"template_type": schema.StringAttribute{
				MarkdownDescription: "Type of template (`container` or `vm`), derived from the source.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region inherited from the source instance.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current status: `creating`, `ready`, `error`, or `deleting`.",
				Computed:            true,
			},
			"error_message": schema.StringAttribute{
				MarkdownDescription: "Last error message if status is `error`.",
				Computed:            true,
			},
			"disk_gb": schema.Int64Attribute{
				MarkdownDescription: "Snapshot disk size in gibibytes (set once `ready`).",
				Computed:            true,
			},
			"source_instance_id": schema.StringAttribute{
				MarkdownDescription: "Server-side reference to the source container/VM (matches `source_container_id` or `source_vm_id`).",
				Computed:            true,
			},
			"source_instance_type": schema.StringAttribute{
				MarkdownDescription: "Source instance type (`container` or `vm`).",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (ISO-8601).",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "Last update timestamp (ISO-8601).",
				Computed:            true,
			},
		},
	}
}

func (r *customTemplateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *customTemplateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan customTemplateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	containerID := plan.SourceContainerID.ValueString()
	vmID := plan.SourceVMID.ValueString()
	if containerID == "" && vmID == "" {
		resp.Diagnostics.AddError(
			"Missing source",
			"Exactly one of `source_container_id` or `source_vm_id` must be set.",
		)
		return
	}

	createReq := client.CustomTemplateCreateRequest{Name: plan.Name.ValueString()}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		desc := plan.Description.ValueString()
		createReq.Description = &desc
	}

	var (
		tpl *client.CustomTemplate
		err error
	)
	if containerID != "" {
		tpl, err = r.client.CreateCustomTemplateFromContainer(ctx, containerID, createReq)
	} else {
		tpl, err = r.client.CreateCustomTemplateFromVm(ctx, vmID, createReq)
	}
	if err != nil {
		resp.Diagnostics.AddError("Custom template creation failed", err.Error())
		return
	}

	state := plan
	r.populateState(&state, tpl)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *customTemplateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state customTemplateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tpl, err := r.client.GetCustomTemplate(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read custom template", err.Error())
		return
	}
	r.populateState(&state, tpl)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *customTemplateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state customTemplateModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.CustomTemplateUpdateRequest{}
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		body.Name = &v
	}
	if !plan.Description.Equal(state.Description) {
		// Description is optional; sending nil clears it on the API side.
		v := plan.Description.ValueString()
		body.Description = &v
	}
	if body.Name == nil && body.Description == nil {
		// Nothing changed (computed attrs only) — keep current state.
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	tpl, err := r.client.UpdateCustomTemplate(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update custom template", err.Error())
		return
	}
	r.populateState(&plan, tpl)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *customTemplateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state customTemplateModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteCustomTemplate(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete custom template", err.Error())
	}
}

func (r *customTemplateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func (r *customTemplateResource) populateState(state *customTemplateModel, tpl *client.CustomTemplate) {
	state.ID = types.StringValue(tpl.ID)
	state.Name = types.StringValue(tpl.Name)
	if tpl.Description != nil {
		state.Description = types.StringValue(*tpl.Description)
	} else {
		state.Description = types.StringNull()
	}
	state.TemplateType = types.StringValue(tpl.TemplateType)
	state.Region = types.StringValue(tpl.Region)
	state.Status = types.StringValue(tpl.Status)
	if tpl.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*tpl.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}
	if tpl.DiskGB != nil {
		state.DiskGB = types.Int64Value(int64(*tpl.DiskGB))
	} else {
		state.DiskGB = types.Int64Null()
	}
	if tpl.SourceInstanceID != nil {
		state.SourceInstanceID = types.StringValue(*tpl.SourceInstanceID)
	} else {
		state.SourceInstanceID = types.StringNull()
	}
	if tpl.SourceInstanceType != nil {
		state.SourceInstanceType = types.StringValue(*tpl.SourceInstanceType)
	} else {
		state.SourceInstanceType = types.StringNull()
	}
	state.CreatedAt = types.StringValue(tpl.CreatedAt)
	state.UpdatedAt = types.StringValue(tpl.UpdatedAt)
}
