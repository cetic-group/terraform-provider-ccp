// Package supportsubscription provides the ccp_support_subscription resource —
// manages the current tenant's subscription to a CETIC support plan (vague C6).
//
// Lifecycle:
// - Create / Update : POST /v1/support/subscribe {plan_key}
//   → switches the tenant to the target plan. If the plan is paid and the
//     tenant has no payment method, the API returns 402.
// - Read   : GET /v1/support/subscription → mirrors the current active sub.
// - Delete : POST /v1/support/unsubscribe → tenant falls back on the default
//   `base` plan. The resource is removed from state but the tenant keeps the
//   base plan (which is gratuit and is the implicit default).
//
// Only ONE active subscription per tenant — the resource is meant to be a
// singleton per workspace.
package supportsubscription

import (
	"context"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &supportSubscriptionResource{}
	_ resource.ResourceWithConfigure   = &supportSubscriptionResource{}
	_ resource.ResourceWithImportState = &supportSubscriptionResource{}
)

func New() resource.Resource { return &supportSubscriptionResource{} }

type supportSubscriptionResource struct{ client *client.Client }

type supportSubscriptionModel struct {
	ID        types.String `tfsdk:"id"`
	TenantID  types.String `tfsdk:"tenant_id"`
	PlanKey   types.String `tfsdk:"plan_key"`
	StartedAt types.String `tfsdk:"started_at"`
	Reason    types.String `tfsdk:"reason"`
}

func (r *supportSubscriptionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_support_subscription"
}

func (r *supportSubscriptionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Active CETIC support subscription for the current tenant. Switches the tenant to the named plan; only one subscription can exist at a time. Destroying the resource downgrades to the default `base` plan.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description:   "Subscription row ID (UUID).",
			},
			"tenant_id": schema.StringAttribute{
				Computed:    true,
				Description: "Resolved tenant ID.",
			},
			"plan_key": schema.StringAttribute{
				Required:    true,
				Description: "Target plan key: `base`, `standard`, `premium`, or a custom key configured by an admin.",
			},
			"started_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp when the current subscription started.",
			},
			"reason": schema.StringAttribute{
				Computed:    true,
				Description: "Reason for the last switch (`user_changed`, `admin_grant`, `initial`).",
			},
		},
	}
}

func (r *supportSubscriptionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *supportSubscriptionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan supportSubscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sub, err := r.client.SubscribeSupportPlan(ctx, plan.PlanKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("SubscribeSupportPlan failed", err.Error())
		return
	}
	state := subFromAPI(sub)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *supportSubscriptionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state supportSubscriptionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cur, err := r.client.GetCurrentSupportSubscription(ctx)
	if err != nil {
		resp.Diagnostics.AddError("GetCurrentSupportSubscription failed", err.Error())
		return
	}
	if cur.Subscription == nil {
		// No active sub — tenant fell back to nothing/base outside Terraform.
		resp.State.RemoveResource(ctx)
		return
	}
	state = subFromAPI(cur.Subscription)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *supportSubscriptionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan supportSubscriptionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sub, err := r.client.SubscribeSupportPlan(ctx, plan.PlanKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("SubscribeSupportPlan failed", err.Error())
		return
	}
	state := subFromAPI(sub)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *supportSubscriptionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Downgrade to base. Best-effort — if the API has already moved the tenant
	// to base for some reason, this is idempotent server-side.
	if _, err := r.client.UnsubscribeSupportPlan(ctx); err != nil {
		resp.Diagnostics.AddError("UnsubscribeSupportPlan failed", err.Error())
	}
}

func (r *supportSubscriptionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func subFromAPI(s *client.SupportSubscription) supportSubscriptionModel {
	m := supportSubscriptionModel{
		ID:        types.StringValue(s.ID),
		TenantID:  types.StringValue(s.TenantID),
		PlanKey:   types.StringValue(s.PlanKey),
		StartedAt: types.StringValue(s.StartedAt.Format("2006-01-02T15:04:05Z07:00")),
	}
	if s.Reason != nil {
		m.Reason = types.StringValue(*s.Reason)
	} else {
		m.Reason = types.StringNull()
	}
	return m
}
