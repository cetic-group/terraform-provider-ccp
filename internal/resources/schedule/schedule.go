// Package schedule implements the ccp_schedule Terraform resource — a
// start/stop planner that powers a resource off during declared weekly
// windows and back on outside of them (cost saver for non-production
// workloads: turn a VM, container, scale set, Kubernetes node pool or
// database instance off nights and week-ends).
//
// The target is addressed polymorphically by (resource_type, resource_id).
// Both are immutable — re-pointing a schedule at a different resource forces
// a replace (destroy + create). Everything else (name, timezone, enabled,
// windows) is mutable in place via PATCH.
//
// Powering off never destroys the target: it is stopped, not deleted. For a
// database instance the storage is kept — only the compute is scaled to zero.
//
// CRUD semantics :
//   - Create : POST /v1/schedules — the platform validates the windows and
//     rejects flapping schedules with a business 422 (message surfaced as-is).
//   - Read   : GET /v1/schedules/{id}. 404 ⇒ removed from state.
//   - Update : PATCH /v1/schedules/{id} (name / timezone / enabled / windows).
//   - Delete : DELETE /v1/schedules/{id} — powers the target back on. 404 ⇒
//     idempotent no-op.
package schedule

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*scheduleResource)(nil)
	_ resource.ResourceWithConfigure   = (*scheduleResource)(nil)
	_ resource.ResourceWithImportState = (*scheduleResource)(nil)
)

// resourceTypes is the set of targetable resource kinds (mirrors the API
// `resource_type` enum). ccks_node_pool targets an individual node pool, not
// the whole cluster.
var resourceTypes = []string{
	"vm",
	"container",
	"vm_scale_set",
	"container_scale_set",
	"ccks_node_pool",
	"db_instance",
}

// New returns the resource factory used by `provider.Resources()`.
func New() resource.Resource { return &scheduleResource{} }

type scheduleResource struct{ client *client.Client }

type scheduleResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	ResourceType             types.String `tfsdk:"resource_type"`
	ResourceID               types.String `tfsdk:"resource_id"`
	Timezone                 types.String `tfsdk:"timezone"`
	Enabled                  types.Bool   `tfsdk:"enabled"`
	Windows                  types.List   `tfsdk:"windows"`
	CurrentState             types.String `tfsdk:"current_state"`
	LastTransitionAt         types.String `tfsdk:"last_transition_at"`
	EstimatedMonthlyFeeCents types.Int64  `tfsdk:"estimated_monthly_fee_cents"`
}

// scheduleWindowModel mirrors one element of the `windows` ListNestedAttribute.
type scheduleWindowModel struct {
	StartDay  types.Int64 `tfsdk:"start_day"`
	StartHour types.Int64 `tfsdk:"start_hour"`
	EndDay    types.Int64 `tfsdk:"end_day"`
	EndHour   types.Int64 `tfsdk:"end_hour"`
}

func (r *scheduleResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_schedule"
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud start/stop schedule — a weekly planner that powers a " +
			"resource **off** during the declared windows and back **on** outside of them. Typical use: " +
			"turn a non-production VM, container, scale set, Kubernetes node pool or database instance off " +
			"at night and over week-ends to save on compute.\n\n" +
			"~> **Stopping is not destroying.** A scheduled-off resource is powered down, never deleted. " +
			"For a database instance the stored data is kept — only the compute is scaled to zero — so you " +
			"keep paying for storage while compute charges pause.\n\n" +
			"~> **`resource_type` and `resource_id` are immutable.** Re-pointing a schedule at a different " +
			"target forces a destroy + create.\n\n" +
			"~> **Windows are validated to prevent flapping.** The platform rejects windows shorter than " +
			"one hour, more than two on/off cycles per day, or overlapping windows with a clear error — " +
			"short or frequent toggles bring no saving because usage is billed by the hour.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the schedule.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable label, unique within the org (max 63 chars). Mutable in place.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 63)},
			},
			"resource_type": schema.StringAttribute{
				MarkdownDescription: "Kind of resource the schedule drives: `vm`, `container`, `vm_scale_set`, " +
					"`container_scale_set`, `ccks_node_pool` (a single Kubernetes node pool, not the whole " +
					"cluster) or `db_instance`. **Immutable** — changing forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(resourceTypes...),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"resource_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the target resource. For `ccks_node_pool` this is the node pool " +
					"id (`ccp_k8s_node_pool.id`), not the cluster id. **Immutable** — changing forces replacement.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"timezone": schema.StringAttribute{
				MarkdownDescription: "IANA timezone the windows are interpreted in (e.g. `Europe/Paris`, " +
					"`Africa/Abidjan`). Defaults to `Europe/Paris`. Mutable in place.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("Europe/Paris"),
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the schedule actively drives the target. When `false` the plan " +
					"is kept but never applied (the resource stays in its current power state). Defaults to " +
					"`true`. Mutable in place.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"windows": schema.ListNestedAttribute{
				MarkdownDescription: "One or more **weekly OFF intervals**. The target is powered off during " +
					"each interval and on outside of it. An interval runs from `start_day`/`start_hour` to " +
					"`end_day`/`end_hour` (exclusive); when the end is earlier than the start it wraps across " +
					"the week-end (Sunday → Monday). Example: `start_day=4, start_hour=20, end_day=0, " +
					"end_hour=8` powers off from Friday 20:00 to Monday 08:00. Mutable in place.",
				Required: true,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"start_day": schema.Int64Attribute{
							MarkdownDescription: "Day the OFF interval starts: `0`=Monday … `6`=Sunday.",
							Required:            true,
							Validators:          []validator.Int64{int64validator.Between(0, 6)},
						},
						"start_hour": schema.Int64Attribute{
							MarkdownDescription: "Hour the OFF interval starts (`0..24`, whole hour, `HH:00`).",
							Required:            true,
							Validators:          []validator.Int64{int64validator.Between(0, 24)},
						},
						"end_day": schema.Int64Attribute{
							MarkdownDescription: "Day the OFF interval ends: `0`=Monday … `6`=Sunday.",
							Required:            true,
							Validators:          []validator.Int64{int64validator.Between(0, 6)},
						},
						"end_hour": schema.Int64Attribute{
							MarkdownDescription: "Hour the OFF interval ends (`0..24`, whole hour, `HH:00`).",
							Required:            true,
							Validators:          []validator.Int64{int64validator.Between(0, 24)},
						},
					},
				},
			},
			"current_state": schema.StringAttribute{
				MarkdownDescription: "Last desired power state applied by the platform: `on` or `off`.",
				Computed:            true,
				// No UseStateForUnknown: the state evolves on its own as windows
				// come and go — it must stay known-after-apply.
			},
			"last_transition_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last power transition the platform applied, " +
					"or null if none yet.",
				Computed: true,
			},
			"estimated_monthly_fee_cents": schema.Int64Attribute{
				MarkdownDescription: "Estimated monthly scheduler fee in cents (number of driven instances × " +
					"the per-instance rate). Read-only.",
				Computed: true,
			},
		},
	}
}

func (r *scheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider — please report it.", req.ProviderData))
		return
	}
	r.client = c
}

// windowObjectAttrTypes is the attribute-type map of a single window object,
// used when building the framework List value in applyScheduleToModel.
func windowObjectAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"start_day":  types.Int64Type,
		"start_hour": types.Int64Type,
		"end_day":    types.Int64Type,
		"end_hour":   types.Int64Type,
	}
}

// windowsFromModel decodes the framework list into API windows.
func windowsFromModel(ctx context.Context, list types.List) ([]client.ScheduleWindow, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := []client.ScheduleWindow{}
	if list.IsNull() || list.IsUnknown() {
		return out, diags
	}
	var models []scheduleWindowModel
	diags.Append(list.ElementsAs(ctx, &models, false)...)
	if diags.HasError() {
		return out, diags
	}
	for i := range models {
		out = append(out, client.ScheduleWindow{
			StartDay:  int(models[i].StartDay.ValueInt64()),
			StartHour: int(models[i].StartHour.ValueInt64()),
			EndDay:    int(models[i].EndDay.ValueInt64()),
			EndHour:   int(models[i].EndHour.ValueInt64()),
		})
	}
	return out, diags
}

// applyScheduleToModel maps an API Schedule onto the Terraform model.
func applyScheduleToModel(ctx context.Context, m *scheduleResourceModel, s *client.Schedule) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(s.ID)
	m.Name = types.StringValue(s.Name)
	m.ResourceType = types.StringValue(s.ResourceType)
	m.ResourceID = types.StringValue(s.ResourceID)
	m.Timezone = types.StringValue(s.Timezone)
	m.Enabled = types.BoolValue(s.Enabled)
	m.CurrentState = types.StringValue(s.CurrentState)
	m.EstimatedMonthlyFeeCents = types.Int64Value(s.EstimatedMonthlyFeeCents)

	if s.LastTransitionAt != nil {
		m.LastTransitionAt = types.StringValue(*s.LastTransitionAt)
	} else {
		m.LastTransitionAt = types.StringNull()
	}

	winObjType := types.ObjectType{AttrTypes: windowObjectAttrTypes()}
	winVals := make([]scheduleWindowModel, 0, len(s.Windows))
	for i := range s.Windows {
		winVals = append(winVals, scheduleWindowModel{
			StartDay:  types.Int64Value(int64(s.Windows[i].StartDay)),
			StartHour: types.Int64Value(int64(s.Windows[i].StartHour)),
			EndDay:    types.Int64Value(int64(s.Windows[i].EndDay)),
			EndHour:   types.Int64Value(int64(s.Windows[i].EndHour)),
		})
	}
	winList, d := types.ListValueFrom(ctx, winObjType, winVals)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Windows = winList

	return diags
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	windows, diags := windowsFromModel(ctx, plan.Windows)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := client.ScheduleCreatePayload{
		Name:         plan.Name.ValueString(),
		ResourceType: plan.ResourceType.ValueString(),
		ResourceID:   plan.ResourceID.ValueString(),
		Windows:      windows,
	}
	// timezone / enabled are Optional+Computed with a Default, so they are
	// always known at plan time — send them explicitly.
	if !plan.Timezone.IsNull() && !plan.Timezone.IsUnknown() {
		v := plan.Timezone.ValueString()
		payload.Timezone = &v
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		v := plan.Enabled.ValueBool()
		payload.Enabled = &v
	}

	created, err := r.client.CreateSchedule(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create CETIC Cloud schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(applyScheduleToModel(ctx, &plan, created)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetSchedule(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(applyScheduleToModel(ctx, &state, got)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state scheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Belt-and-suspenders: resource_type / resource_id carry RequiresReplace,
	// so Update should never see them change.
	if !plan.ResourceType.Equal(state.ResourceType) || !plan.ResourceID.Equal(state.ResourceID) {
		resp.Diagnostics.AddError("target is immutable",
			"Changing `resource_type` or `resource_id` should have triggered a replace — please file a bug.")
		return
	}

	id := state.ID.ValueString()
	var upd client.ScheduleUpdatePayload
	patchNeeded := false

	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
		patchNeeded = true
	}
	if !plan.Timezone.Equal(state.Timezone) {
		v := plan.Timezone.ValueString()
		upd.Timezone = &v
		patchNeeded = true
	}
	if !plan.Enabled.Equal(state.Enabled) {
		v := plan.Enabled.ValueBool()
		upd.Enabled = &v
		patchNeeded = true
	}
	if !plan.Windows.Equal(state.Windows) {
		windows, diags := windowsFromModel(ctx, plan.Windows)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		upd.Windows = &windows
		patchNeeded = true
	}

	if !patchNeeded {
		// Nothing mutable changed — persist the plan as-is.
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}

	updated, err := r.client.UpdateSchedule(ctx, id, upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(applyScheduleToModel(ctx, &plan, updated)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSchedule(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete schedule", err.Error())
	}
}

// ImportState rebinds an existing schedule by UUID.
func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
