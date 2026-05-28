// Package commit provides the ccp_commit resource — manages a tenant's
// monthly (-10%) or yearly (-20%) discount commitment.
package commit

import (
	"context"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
)

var (
	_ resource.Resource                = &commitResource{}
	_ resource.ResourceWithConfigure   = &commitResource{}
	_ resource.ResourceWithImportState = &commitResource{}
)

func New() resource.Resource { return &commitResource{} }

type commitResource struct{ client *client.Client }

type commitModel struct {
	ID          types.String `tfsdk:"id"`
	TenantID    types.String `tfsdk:"tenant_id"`
	CommitType  types.String `tfsdk:"commit_type"`
	DiscountPct types.Int64  `tfsdk:"discount_pct"`
	StartAt     types.String `tfsdk:"start_at"`
	EndAt       types.String `tfsdk:"end_at"`
	AutoRenew   types.Bool   `tfsdk:"auto_renew"`
	CanceledAt  types.String `tfsdk:"canceled_at"`
}

func (r *commitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_commit"
}

func (r *commitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Commitment-based discount on the tenant's overall consumption. `monthly` = -10% over 30 days, `yearly` = -20% over 365 days. Cancelling on delete keeps the discount active until end_at, then stops auto-renew.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tenant_id": schema.StringAttribute{
				Computed: true,
			},
			"commit_type": schema.StringAttribute{
				Required:    true,
				Description: "`monthly` (-10%) or `yearly` (-20%). Changing this requires a new resource.",
				Validators: []validator.String{
					stringvalidator.OneOf("monthly", "yearly"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"discount_pct": schema.Int64Attribute{
				Computed:    true,
				Description: "Resolved discount % (10 or 20).",
			},
			"start_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp.",
			},
			"end_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp.",
			},
			"auto_renew": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "If true, renews automatically at end_at.",
				Default:     booldefault.StaticBool(true),
			},
			"canceled_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp of cancellation, or empty if active.",
			},
		},
	}
}

func (r *commitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *commitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan commitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateCommit(ctx, client.CommitCreateRequest{
		CommitType: plan.CommitType.ValueString(),
		AutoRenew:  plan.AutoRenew.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("CreateCommit failed", err.Error())
		return
	}
	state := commitFromAPI(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *commitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state commitModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetCommit(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("GetCommit failed", err.Error())
		return
	}
	state = commitFromAPI(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *commitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// La seule chose mutable côté API est l'autorenew — pas exposé en PATCH.
	// On read-back simplement pour matcher l'état.
	var plan commitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.client.GetCommit(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("GetCommit failed", err.Error())
		return
	}
	state := commitFromAPI(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *commitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state commitModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.CancelCommit(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("CancelCommit failed", err.Error())
	}
}

func (r *commitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

func commitFromAPI(c *client.Commit) commitModel {
	m := commitModel{
		ID:          types.StringValue(c.ID),
		TenantID:    types.StringValue(c.TenantID),
		CommitType:  types.StringValue(c.CommitType),
		DiscountPct: types.Int64Value(int64(c.DiscountPct)),
		StartAt:     types.StringValue(c.StartAt.Format("2006-01-02T15:04:05Z07:00")),
		EndAt:       types.StringValue(c.EndAt.Format("2006-01-02T15:04:05Z07:00")),
		AutoRenew:   types.BoolValue(c.AutoRenew),
	}
	if c.CanceledAt != nil {
		m.CanceledAt = types.StringValue(c.CanceledAt.Format("2006-01-02T15:04:05Z07:00"))
	} else {
		m.CanceledAt = types.StringValue("")
	}
	return m
}
