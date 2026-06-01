// Package budget provides the ccp_budget resource — manages a tenant's
// monthly budget cap + alert thresholds.
package budget

import (
	"context"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                = &budgetResource{}
	_ resource.ResourceWithConfigure   = &budgetResource{}
	_ resource.ResourceWithImportState = &budgetResource{}
)

func New() resource.Resource { return &budgetResource{} }

type budgetResource struct{ client *client.Client }

type budgetModel struct {
	ID                    types.String `tfsdk:"id"`
	TenantID              types.String `tfsdk:"tenant_id"`
	MonthlyBudgetCents    types.Int64  `tfsdk:"monthly_budget_cents"`
	Currency              types.String `tfsdk:"currency"`
	AlertThresholdsPct    types.List   `tfsdk:"alert_thresholds_pct"`
	NotifyEmails          types.List   `tfsdk:"notify_emails"`
	HardStopAt100         types.Bool   `tfsdk:"hard_stop_at_100"`
	LastAlertThresholdPct types.Int64  `tfsdk:"last_alert_threshold_pct"`
	Active                types.Bool   `tfsdk:"active"`
}

func (r *budgetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_budget"
}

func (r *budgetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Monthly budget for the current tenant, with email alerts at configurable thresholds and an optional hard-stop at 100% that blocks resource creation. The actual billing remains hourly — this only adds visibility + safeguards.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tenant_id": schema.StringAttribute{
				Computed:    true,
				Description: "Resolved tenant ID.",
			},
			"monthly_budget_cents": schema.Int64Attribute{
				Required:    true,
				Description: "Monthly cap in EUR cents (ex: `5000` for 50€).",
			},
			"currency": schema.StringAttribute{
				Computed:    true,
				Description: "ISO 4217 currency (always `eur` for now).",
			},
			"alert_thresholds_pct": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Percentage thresholds that trigger alerts (defaults to `[50, 80, 100]` if omitted).",
			},
			"notify_emails": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Email recipients. If empty, the tenant account email is used.",
			},
			"hard_stop_at_100": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, resource creation is blocked once MTD usage reaches the cap.",
				Default:     booldefault.StaticBool(false),
			},
			"last_alert_threshold_pct": schema.Int64Attribute{
				Computed:    true,
				Description: "Most recent threshold that triggered an alert this month (or null).",
			},
			"active": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the budget is currently active.",
			},
		},
	}
}

func (r *budgetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Configure error", "unexpected provider data type")
		return
	}
	r.client = c
}

func (r *budgetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan budgetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	thresholds := listToInt64s(ctx, plan.AlertThresholdsPct)
	if len(thresholds) == 0 {
		thresholds = []int{50, 80, 100}
	}
	emails := listToStrings(ctx, plan.NotifyEmails)
	created, err := r.client.CreateBudget(ctx, client.BudgetCreateRequest{
		MonthlyBudgetCents: int(plan.MonthlyBudgetCents.ValueInt64()),
		AlertThresholdsPct: thresholds,
		NotifyEmails:       emails,
		HardStopAt100:      plan.HardStopAt100.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("CreateBudget failed", err.Error())
		return
	}
	state := budgetFromAPI(ctx, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *budgetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state budgetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := r.client.GetBudget(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("GetBudget failed", err.Error())
		return
	}
	state = budgetFromAPI(ctx, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *budgetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan budgetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	thresholds := listToInt64s(ctx, plan.AlertThresholdsPct)
	if len(thresholds) == 0 {
		thresholds = []int{50, 80, 100}
	}
	emails := listToStrings(ctx, plan.NotifyEmails)
	b, err := r.client.UpdateBudget(ctx, plan.ID.ValueString(), client.BudgetCreateRequest{
		MonthlyBudgetCents: int(plan.MonthlyBudgetCents.ValueInt64()),
		AlertThresholdsPct: thresholds,
		NotifyEmails:       emails,
		HardStopAt100:      plan.HardStopAt100.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("UpdateBudget failed", err.Error())
		return
	}
	state := budgetFromAPI(ctx, b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *budgetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state budgetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteBudget(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("DeleteBudget failed", err.Error())
	}
}

func (r *budgetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func budgetFromAPI(ctx context.Context, b *client.Budget) budgetModel {
	m := budgetModel{
		ID:                 types.StringValue(b.ID),
		TenantID:           types.StringValue(b.TenantID),
		MonthlyBudgetCents: types.Int64Value(int64(b.MonthlyBudgetCents)),
		Currency:           types.StringValue(b.Currency),
		HardStopAt100:      types.BoolValue(b.HardStopAt100),
		Active:             types.BoolValue(b.Active),
	}
	if b.LastAlertThresholdPct != nil {
		m.LastAlertThresholdPct = types.Int64Value(int64(*b.LastAlertThresholdPct))
	} else {
		m.LastAlertThresholdPct = types.Int64Null()
	}
	thresholdsList, _ := basetypes.NewListValueFrom(ctx, types.Int64Type, b.AlertThresholdsPct)
	m.AlertThresholdsPct = thresholdsList
	if b.NotifyEmails == nil {
		b.NotifyEmails = []string{}
	}
	emailsList, _ := basetypes.NewListValueFrom(ctx, types.StringType, b.NotifyEmails)
	m.NotifyEmails = emailsList
	return m
}

func listToInt64s(ctx context.Context, l types.List) []int {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var raw []int64
	_ = l.ElementsAs(ctx, &raw, false)
	out := make([]int, len(raw))
	for i, v := range raw {
		out[i] = int(v)
	}
	return out
}

func listToStrings(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var raw []string
	_ = l.ElementsAs(ctx, &raw, false)
	return raw
}
