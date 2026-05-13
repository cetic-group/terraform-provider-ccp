// Package vmscaleset implements the ccp_vm_scale_set Terraform resource.
//
// Le scale set fait grandir/réduire un pool de VMs QEMU identiques selon
// `desired_instances` (entre min et max). Les VMs individuelles sont
// gérées par CL en interne — pas exposées en TF state pour éviter les drifts
// avec l'auto-repair / auto-scale.
package vmscaleset

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*vmssResource)(nil)
	_ resource.ResourceWithConfigure   = (*vmssResource)(nil)
	_ resource.ResourceWithImportState = (*vmssResource)(nil)
)

func New() resource.Resource { return &vmssResource{} }

type vmssResource struct {
	client *client.Client
}

type vmssResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Region           types.String `tfsdk:"region"`
	Plan             types.String `tfsdk:"plan"`
	Template         types.String `tfsdk:"template"`
	VnetID           types.String `tfsdk:"vnet_id"`
	SSHKeyIDs        types.List   `tfsdk:"ssh_key_ids"`
	UserData         types.String `tfsdk:"user_data"`
	RootPassword     types.String `tfsdk:"root_password"`
	MinInstances     types.Int64  `tfsdk:"min_instances"`
	MaxInstances     types.Int64  `tfsdk:"max_instances"`
	DesiredInstances types.Int64  `tfsdk:"desired_instances"`
	AutoRepair       types.Bool   `tfsdk:"auto_repair"`
	Status           types.String `tfsdk:"status"`
	Tags             types.List   `tfsdk:"tags"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *vmssResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_scale_set"
}

func (r *vmssResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VM Scale Set — a pool of identical VMs " +
			"with min/max bounds and `desired_instances` for hot scaling. Auto-repair re-creates failed containers " +
			"when enabled.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name (1-80 chars, alphanumerics + `-` + `_`).",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 80)},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code (RNN, PAR, ABJ). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Plan: nano | micro | small | medium | large | xlarge. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"template": schema.StringAttribute{
				MarkdownDescription: "VM template key (clé QEMU) (default: `ubuntu-24.04` (VM template clé)). Forces replacement.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("ubuntu-24.04"),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet to join. Forces replacement.",
				Optional:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ssh_key_ids": schema.ListAttribute{
				MarkdownDescription: "List of SSH key UUIDs injected into every replica at boot via cloud-init. Write-only — the API does not return this field on read, so changes force replacement of the whole scale set.",
				ElementType:         types.StringType,
				Optional:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.RequiresReplace(), listplanmodifier.UseStateForUnknown()},
			},
			"user_data": schema.StringAttribute{
				MarkdownDescription: "Cloud-init user-data injected into every replica at boot. Write-only — the API does not return this field on read, so changes force replacement.",
				Optional:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()},
			},
			"root_password": schema.StringAttribute{
				MarkdownDescription: "Root password injected into every replica at first boot. " +
					"**Required** (CCP API ≥ v1.4.0 enforces a non-empty password, 8-128 chars). " +
					"Sensitive: never returned by the API after creation. Forces replacement on change.",
				Required:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(8, 128),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"min_instances": schema.Int64Attribute{
				MarkdownDescription: "Minimum container count (0-50).",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 50)},
			},
			"max_instances": schema.Int64Attribute{
				MarkdownDescription: "Maximum container count (1-50). Must be ≥ min_instances.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 50)},
			},
			"desired_instances": schema.Int64Attribute{
				MarkdownDescription: "Target container count. Must be in [min, max].",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(0, 50)},
			},
			"auto_repair": schema.BoolAttribute{
				MarkdownDescription: "Recreate failed/stopped containers automatically (default: true).",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"status": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *vmssResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = c
}

func stateFromAPI(ctx context.Context, c *client.VMScaleSet) (vmssResourceModel, []string) {
	m := vmssResourceModel{
		ID:               types.StringValue(c.ID),
		Name:             types.StringValue(c.Name),
		Region:           types.StringValue(c.Region),
		Plan:             types.StringValue(c.Plan),
		Template:         types.StringValue(c.Template),
		MinInstances:     types.Int64Value(int64(c.MinInstances)),
		MaxInstances:     types.Int64Value(int64(c.MaxInstances)),
		DesiredInstances: types.Int64Value(int64(c.DesiredInstances)),
		AutoRepair:       types.BoolValue(c.AutoRepair),
		Status:           types.StringValue(c.Status),
		CreatedAt:        types.StringValue(c.CreatedAt.Format("2006-01-02T15:04:05Z07:00")),
	}
	if c.VnetID != nil {
		m.VnetID = types.StringValue(*c.VnetID)
	} else {
		m.VnetID = types.StringNull()
	}
	tags, diag := types.ListValueFrom(ctx, types.StringType, c.Tags)
	var diagStrs []string
	if diag.HasError() {
		for _, d := range diag.Errors() {
			diagStrs = append(diagStrs, d.Summary()+": "+d.Detail())
		}
	}
	m.Tags = tags
	return m, diagStrs
}

func (r *vmssResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vmssResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.VMScaleSetCreateRequest{
		Name:             plan.Name.ValueString(),
		Region:           plan.Region.ValueString(),
		Plan:             plan.Plan.ValueString(),
		Template:         plan.Template.ValueString(),
		MinInstances:     int(plan.MinInstances.ValueInt64()),
		MaxInstances:     int(plan.MaxInstances.ValueInt64()),
		DesiredInstances: int(plan.DesiredInstances.ValueInt64()),
		AutoRepair:       plan.AutoRepair.ValueBool(),
	}
	if !plan.VnetID.IsNull() && !plan.VnetID.IsUnknown() {
		v := plan.VnetID.ValueString()
		createReq.VnetID = &v
	}
	if !plan.SSHKeyIDs.IsNull() && !plan.SSHKeyIDs.IsUnknown() {
		keys := []string{}
		plan.SSHKeyIDs.ElementsAs(ctx, &keys, false)
		createReq.SSHKeyIDs = keys
	}
	if !plan.UserData.IsNull() && !plan.UserData.IsUnknown() {
		v := plan.UserData.ValueString()
		createReq.UserData = &v
	}
	// root_password est Required → toujours présent dans le plan
	rp := plan.RootPassword.ValueString()
	createReq.RootPassword = &rp
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags := []string{}
		plan.Tags.ElementsAs(ctx, &tags, false)
		createReq.Tags = tags
	}

	created, err := r.client.CreateVMScaleSet(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VM Scale Set", err.Error())
		return
	}
	state, diags := stateFromAPI(ctx, created)
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	state.SSHKeyIDs = plan.SSHKeyIDs
	state.UserData = plan.UserData
	// API ne renvoie pas root_password sur read (sensible) ; conserver la
	// valeur du plan dans le state pour pas que le diff montre un retrait.
	state.RootPassword = plan.RootPassword
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *vmssResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vmssResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetVMScaleSet(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VM Scale Set", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, got)
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	newState.SSHKeyIDs = state.SSHKeyIDs
	newState.UserData = state.UserData
	// API ne renvoie pas root_password (sensible) ; conserver l'ancienne
	// valeur du state pour éviter un drift.
	newState.RootPassword = state.RootPassword
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *vmssResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state vmssResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	var upd client.VMScaleSetUpdateRequest
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
	}
	if !plan.MinInstances.Equal(state.MinInstances) {
		v := int(plan.MinInstances.ValueInt64())
		upd.MinInstances = &v
	}
	if !plan.MaxInstances.Equal(state.MaxInstances) {
		v := int(plan.MaxInstances.ValueInt64())
		upd.MaxInstances = &v
	}
	if !plan.DesiredInstances.Equal(state.DesiredInstances) {
		v := int(plan.DesiredInstances.ValueInt64())
		upd.DesiredInstances = &v
	}
	if !plan.AutoRepair.Equal(state.AutoRepair) {
		v := plan.AutoRepair.ValueBool()
		upd.AutoRepair = &v
	}
	if !plan.Tags.Equal(state.Tags) {
		tags := []string{}
		if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
			plan.Tags.ElementsAs(ctx, &tags, false)
		}
		upd.Tags = tags
	}

	updated, err := r.client.UpdateVMScaleSet(ctx, id, upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update VM Scale Set", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, updated)
	newState.SSHKeyIDs = plan.SSHKeyIDs
	newState.UserData = plan.UserData
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *vmssResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vmssResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVMScaleSet(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VM Scale Set", err.Error())
		return
	}
}

func (r *vmssResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
