// Package schedule implements the ccp_schedule data source — looks up an
// existing start/stop schedule by `id` or by `name` (exactly one must be
// provided) and exposes its target, windows and computed power state / fee.
package schedule

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*scheduleDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*scheduleDataSource)(nil)
)

// New returns the data source factory used by `provider.DataSources()`.
func New() datasource.DataSource { return &scheduleDataSource{} }

type scheduleDataSource struct{ client *client.Client }

type scheduleDataSourceModel struct {
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

func windowObjectAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"start_day":  types.Int64Type,
		"start_hour": types.Int64Type,
		"end_day":    types.Int64Type,
		"end_hour":   types.Int64Type,
	}
}

func (d *scheduleDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_schedule"
}

func (d *scheduleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a CETIC Cloud start/stop schedule by `id` or by `name`. Exactly one of " +
			"`id` or `name` must be provided.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the schedule. Conflicts with `name`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Schedule name, unique within the org. Conflicts with `id`.",
				Optional:            true,
				Computed:            true,
			},
			"resource_type": schema.StringAttribute{
				MarkdownDescription: "Kind of driven resource: `vm`, `container`, `vm_scale_set`, " +
					"`container_scale_set`, `ccks_node_pool` or `db_instance`.",
				Computed: true,
			},
			"resource_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the driven resource.",
				Computed:            true,
			},
			"timezone": schema.StringAttribute{
				MarkdownDescription: "IANA timezone the windows are interpreted in.",
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the schedule actively drives the target.",
				Computed:            true,
			},
			"windows": schema.ListNestedAttribute{
				MarkdownDescription: "Weekly OFF intervals (the resource is powered off during each interval).",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"start_day":  schema.Int64Attribute{MarkdownDescription: "Start day (`0`=Monday … `6`=Sunday).", Computed: true},
						"start_hour": schema.Int64Attribute{MarkdownDescription: "Start hour (`0..24`).", Computed: true},
						"end_day":    schema.Int64Attribute{MarkdownDescription: "End day (`0`=Monday … `6`=Sunday).", Computed: true},
						"end_hour":   schema.Int64Attribute{MarkdownDescription: "End hour (`0..24`).", Computed: true},
					},
				},
			},
			"current_state": schema.StringAttribute{
				MarkdownDescription: "Last desired power state applied: `on` or `off`.",
				Computed:            true,
			},
			"last_transition_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last power transition, or null.",
				Computed:            true,
			},
			"estimated_monthly_fee_cents": schema.Int64Attribute{
				MarkdownDescription: "Estimated monthly scheduler fee in cents.",
				Computed:            true,
			},
		},
	}
}

func (d *scheduleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *scheduleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg scheduleDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name`, not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name` to look up a schedule.")
		return
	}

	var found *client.Schedule
	var err error
	if hasID {
		found, err = d.client.GetSchedule(ctx, cfg.ID.ValueString())
	} else {
		found, err = d.client.GetScheduleByName(ctx, cfg.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to look up schedule", err.Error())
		return
	}

	state := scheduleDataSourceModel{
		ID:                       types.StringValue(found.ID),
		Name:                     types.StringValue(found.Name),
		ResourceType:             types.StringValue(found.ResourceType),
		ResourceID:               types.StringValue(found.ResourceID),
		Timezone:                 types.StringValue(found.Timezone),
		Enabled:                  types.BoolValue(found.Enabled),
		CurrentState:             types.StringValue(found.CurrentState),
		EstimatedMonthlyFeeCents: types.Int64Value(found.EstimatedMonthlyFeeCents),
	}
	if found.LastTransitionAt != nil {
		state.LastTransitionAt = types.StringValue(*found.LastTransitionAt)
	} else {
		state.LastTransitionAt = types.StringNull()
	}

	winObjType := types.ObjectType{AttrTypes: windowObjectAttrTypes()}
	type winModel struct {
		StartDay  types.Int64 `tfsdk:"start_day"`
		StartHour types.Int64 `tfsdk:"start_hour"`
		EndDay    types.Int64 `tfsdk:"end_day"`
		EndHour   types.Int64 `tfsdk:"end_hour"`
	}
	winVals := make([]winModel, 0, len(found.Windows))
	for i := range found.Windows {
		winVals = append(winVals, winModel{
			StartDay:  types.Int64Value(int64(found.Windows[i].StartDay)),
			StartHour: types.Int64Value(int64(found.Windows[i].StartHour)),
			EndDay:    types.Int64Value(int64(found.Windows[i].EndDay)),
			EndHour:   types.Int64Value(int64(found.Windows[i].EndHour)),
		})
	}
	winList, d2 := types.ListValueFrom(ctx, winObjType, winVals)
	resp.Diagnostics.Append(d2...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Windows = winList

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
