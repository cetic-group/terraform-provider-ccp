// Package vmsnapshot implements the ccp_vm_snapshot resource.
//
// Snapshot d'une VM QEMU via qm snapshot (Ceph RBD). La création est
// asynchrone — le resource bloque jusqu'à status=available.
// Note TF : un snapshot est immutable (pas d'Update). Pour modifier sa
// description ou le renommer, il faut supprimer + recréer.
package vmsnapshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*snapResource)(nil)
	_ resource.ResourceWithConfigure   = (*snapResource)(nil)
	_ resource.ResourceWithImportState = (*snapResource)(nil)
)

func New() resource.Resource { return &snapResource{} }

type snapResource struct{ client *client.Client }

type snapModel struct {
	ID          types.String `tfsdk:"id"`
	VMID        types.String `tfsdk:"vm_id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Status      types.String `tfsdk:"status"`
	SizeBytes   types.Int64  `tfsdk:"size_bytes"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *snapResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vm_snapshot"
}

func (r *snapResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Snapshot d'une VM QEMU (Ceph RBD via `qm snapshot`). " +
			"Immutable — toute modification force la recréation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vm_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status":     schema.StringAttribute{Computed: true},
			"size_bytes": schema.Int64Attribute{Computed: true},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *snapResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = c
}

func stateFrom(s *client.VmSnapshot) snapModel {
	m := snapModel{
		ID:        types.StringValue(s.ID),
		VMID:      types.StringValue(s.VmInstanceID),
		Name:      types.StringValue(s.Name),
		Status:    types.StringValue(s.Status),
		CreatedAt: types.StringValue(s.CreatedAt),
	}
	if s.Description != nil {
		m.Description = types.StringValue(*s.Description)
	} else {
		m.Description = types.StringNull()
	}
	if s.SizeBytes != nil {
		m.SizeBytes = types.Int64Value(*s.SizeBytes)
	} else {
		m.SizeBytes = types.Int64Null()
	}
	return m
}

func (r *snapResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan snapModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cr := client.VmSnapshotCreateRequest{Name: plan.Name.ValueString()}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		v := plan.Description.ValueString()
		cr.Description = &v
	}
	snap, err := r.client.CreateVmSnapshot(ctx, plan.VMID.ValueString(), cr)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VM snapshot", err.Error())
		return
	}
	// Poll until available (max 5 min)
	deadline := time.Now().Add(5 * time.Minute)
	for snap.Status == "creating" {
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Second)
		latest, gerr := r.client.GetVmSnapshot(ctx, plan.VMID.ValueString(), snap.ID)
		if gerr == nil {
			snap = latest
		}
	}
	state := stateFrom(snap)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *snapResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state snapModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	snap, err := r.client.GetVmSnapshot(ctx, state.VMID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read VM snapshot", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, stateFrom(snap))...)
}

func (r *snapResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// All fields require replace — Update is never called.
	resp.Diagnostics.AddError("Immutable resource", "VM snapshots are immutable. Use replace.")
}

func (r *snapResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state snapModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteVmSnapshot(ctx, state.VMID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete VM snapshot", err.Error())
		return
	}
}

func (r *snapResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: vm_id/snapshot_id
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "Format: vm_id/snapshot_id")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("vm_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
