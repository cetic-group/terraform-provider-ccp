// Package supportticket implements the ccp_support_ticket Terraform resource.
//
// Use-case IaC limité — utile pour automatiser des tickets d'incident
// (monitoring → ouverture auto). Update/Delete pas exposés en TF (on ne
// supprime pas un ticket via TF, on le ferme via la console/API).
package supportticket

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*ticketResource)(nil)
	_ resource.ResourceWithConfigure   = (*ticketResource)(nil)
	_ resource.ResourceWithImportState = (*ticketResource)(nil)
)

func New() resource.Resource { return &ticketResource{} }

type ticketResource struct{ client *client.Client }

type ticketResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Subject   types.String `tfsdk:"subject"`
	Body      types.String `tfsdk:"body"`
	Category  types.String `tfsdk:"category"`
	Priority  types.String `tfsdk:"priority"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *ticketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_support_ticket"
}

func (r *ticketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Open a CETIC Cloud support ticket. Useful for monitoring → auto " +
			"ticket creation. The body is set at creation time only (replies via API/console). " +
			"Destroying the resource does NOT close the ticket — close it manually.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"subject": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"body": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"category": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("bug", "feature", "billing", "network", "infra", "question"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"priority": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("normal"),
				Validators: []validator.String{
					stringvalidator.OneOf("low", "normal", "high", "urgent"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
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

func (r *ticketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ticketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ticketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateSupportTicket(ctx, client.SupportTicketCreateRequest{
		Subject:  plan.Subject.ValueString(),
		Body:     plan.Body.ValueString(),
		Category: plan.Category.ValueString(),
		Priority: plan.Priority.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create support ticket", err.Error())
		return
	}
	plan.ID = types.StringValue(created.ID)
	plan.Status = types.StringValue(created.Status)
	plan.CreatedAt = types.StringValue(created.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ticketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ticketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetSupportTicket(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read support ticket", err.Error())
		return
	}
	state.Subject = types.StringValue(got.Subject)
	state.Status = types.StringValue(got.Status)
	state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ticketResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "All ticket attributes force replacement.")
}

// Delete : NO-OP — un ticket reste tracé côté CL même après destroy. L'API
// ne supporte pas la suppression de tickets ; on retire juste la resource du state.
func (r *ticketResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *ticketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
