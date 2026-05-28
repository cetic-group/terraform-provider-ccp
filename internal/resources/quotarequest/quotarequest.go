// Package quotarequest implements the ccp_quota_request Terraform resource.
//
// Demande d'augmentation de quota self-service. **One-shot** (pas de PATCH côté
// API — l'admin approve/reject puis la demande est figée). Destroy = retire du
// state TF, ne supprime pas la demande côté API.
package quotarequest

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*qrResource)(nil)
	_ resource.ResourceWithConfigure   = (*qrResource)(nil)
	_ resource.ResourceWithImportState = (*qrResource)(nil)
)

func New() resource.Resource { return &qrResource{} }

type qrResource struct{ client *client.Client }

type qrResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Field          types.String `tfsdk:"field"`
	RequestedValue types.Int64  `tfsdk:"requested_value"`
	Reason         types.String `tfsdk:"reason"`
	CurrentValue   types.Int64  `tfsdk:"current_value"`
	Status         types.String `tfsdk:"status"`
	AdminNote      types.String `tfsdk:"admin_note"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (r *qrResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_quota_request"
}

func (r *qrResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Self-service quota increase request. The admin approves/rejects " +
			"out-of-band — `status` reflects the current state. Destroying the resource removes it " +
			"from TF state but doesn't recall the request.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"field": schema.StringAttribute{
				MarkdownDescription: "Champ quota visé (ex: `max_containers`, `max_cores`, `max_memory_mb`).",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"requested_value": schema.Int64Attribute{
				Required:      true,
				PlanModifiers: []planmodifier.Int64{},
			},
			"reason": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"current_value": schema.Int64Attribute{
				Computed: true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "pending | approved | rejected. Polling après création possible côté Terraform en utilisant `data.ccp_quota_request`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"admin_note": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *qrResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Got %T", req.ProviderData))
		return
	}
	r.client = c
}

func setState(m *qrResourceModel, q *client.QuotaRequest) {
	m.ID = types.StringValue(q.ID)
	m.Field = types.StringValue(q.Field)
	m.RequestedValue = types.Int64Value(int64(q.RequestedValue))
	m.CurrentValue = types.Int64Value(int64(q.CurrentValue))
	m.Status = types.StringValue(q.Status)
	m.CreatedAt = types.StringValue(q.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	if q.Reason != nil {
		m.Reason = types.StringValue(*q.Reason)
	} else {
		m.Reason = types.StringNull()
	}
	if q.AdminNote != nil {
		m.AdminNote = types.StringValue(*q.AdminNote)
	} else {
		m.AdminNote = types.StringNull()
	}
}

func (r *qrResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan qrResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.QuotaRequestCreateRequest{
		Field:          plan.Field.ValueString(),
		RequestedValue: int(plan.RequestedValue.ValueInt64()),
	}
	if !plan.Reason.IsNull() && !plan.Reason.IsUnknown() {
		v := plan.Reason.ValueString()
		createReq.Reason = &v
	}
	created, err := r.client.CreateQuotaRequest(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create quota request", err.Error())
		return
	}
	setState(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *qrResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state qrResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetQuotaRequest(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read quota request", err.Error())
		return
	}
	setState(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *qrResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "Quota requests are immutable; status is updated by the admin out-of-band.")
}

// Delete : NO-OP — l'API ne propose pas de DELETE sur quota_requests
// (l'admin reject une demande au lieu). On retire juste du state TF.
func (r *qrResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *qrResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
