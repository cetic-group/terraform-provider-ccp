// Package loadbalancer implements the ccp_load_balancer Terraform resource.
//
// Le schema TF couvre le cycle de vie du LB (create, name/tags update, delete)
// et l'attachment d'IP publique. Les listeners et backends sont **dynamiques**
// par nature (le client peut en ajouter/retirer depuis l'UI ou la CLI) — les
// gérer dans le state TF causerait des drifts permanents. Pour ces sous-ressources,
// préférer la CLI `lake lb` ou des appels API directs après `terraform apply`.
//
// Provisioning asynchrone : `Create` poll le status jusqu'à ACTIVE (max 5 min).
// `vip_address` et `public_ip_address` sont disponibles seulement après le
// provisionnement complet.
package loadbalancer

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
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
	_ resource.Resource                = (*lbResource)(nil)
	_ resource.ResourceWithConfigure   = (*lbResource)(nil)
	_ resource.ResourceWithImportState = (*lbResource)(nil)
)

func New() resource.Resource { return &lbResource{} }

type lbResource struct {
	client *client.Client
}

type lbResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	VnetID          types.String `tfsdk:"vnet_id"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	VIPAddress      types.String `tfsdk:"vip_address"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
	Status          types.String `tfsdk:"status"`
	Tags            types.List   `tfsdk:"tags"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

func (r *lbResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer"
}

func (r *lbResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud Load Balancer (HAProxy + Keepalived) attached to a VNet. " +
			"Supports public IP attachment via `public_ip_id`. Listeners and backends are **NOT** managed in this " +
			"resource — use the CLI `lake lb` or the console for dynamic backend management.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the load balancer.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (1-100 chars).",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code (RNN, PAR, ABJ, ...). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet the LB joins. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of an allocated `ccp_public_ip` to attach as floating VIP. " +
					"Set/unset to attach/detach. Use `ccp_public_ip` resource to manage the IP itself.",
				Optional: true,
			},
			"vip_address": schema.StringAttribute{
				MarkdownDescription: "Private VIP address on the VNet (Keepalived floating). Available once status=active.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IP attached (resolved from `public_ip_id`). Empty when no IP attached.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Provisioning status: provisioning | active | updating | error | deleting.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form tags.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *lbResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// stateFromAPI populates a model from a *client.LoadBalancer.
func stateFromAPI(ctx context.Context, lb *client.LoadBalancer) (lbResourceModel, []string) {
	m := lbResourceModel{
		ID:        types.StringValue(lb.ID),
		Name:      types.StringValue(lb.Name),
		Region:    types.StringValue(lb.Region),
		VnetID:    types.StringValue(lb.VnetID),
		Status:    types.StringValue(lb.Status),
		CreatedAt: types.StringValue(lb.CreatedAt),
	}
	if lb.PublicIPID != nil {
		m.PublicIPID = types.StringValue(*lb.PublicIPID)
	} else {
		m.PublicIPID = types.StringNull()
	}
	if lb.VIPAddress != nil {
		m.VIPAddress = types.StringValue(*lb.VIPAddress)
	} else {
		m.VIPAddress = types.StringNull()
	}
	if lb.PublicIPAddress != nil {
		m.PublicIPAddress = types.StringValue(*lb.PublicIPAddress)
	} else {
		m.PublicIPAddress = types.StringNull()
	}
	tags, diag := types.ListValueFrom(ctx, types.StringType, lb.Tags)
	var diagStrs []string
	if diag.HasError() {
		for _, d := range diag.Errors() {
			diagStrs = append(diagStrs, d.Summary()+": "+d.Detail())
		}
	}
	m.Tags = tags
	return m, diagStrs
}

func (r *lbResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan lbResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.LoadBalancerCreateRequest{
		Name:   plan.Name.ValueString(),
		Region: plan.Region.ValueString(),
		VnetID: plan.VnetID.ValueString(),
	}
	if !plan.PublicIPID.IsNull() && !plan.PublicIPID.IsUnknown() {
		v := plan.PublicIPID.ValueString()
		createReq.PublicIPID = &v
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags := []string{}
		plan.Tags.ElementsAs(ctx, &tags, false)
		createReq.Tags = tags
	}

	created, err := r.client.CreateLoadBalancer(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Load Balancer", err.Error())
		return
	}

	// Poll until status=active or error (timeout 5 min)
	final, err := pollUntilReady(ctx, r.client, created.ID, 5*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("LB provisioning timed out or failed", err.Error())
		return
	}

	state, diags := stateFromAPI(ctx, final)
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *lbResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state lbResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetLoadBalancer(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read Load Balancer", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, got)
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *lbResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state lbResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// 1. Name + tags via PATCH
	var updReq client.LoadBalancerUpdateRequest
	patchNeeded := false
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		updReq.Name = &v
		patchNeeded = true
	}
	if !plan.Tags.Equal(state.Tags) {
		tags := []string{}
		if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
			plan.Tags.ElementsAs(ctx, &tags, false)
		}
		updReq.Tags = tags
		patchNeeded = true
	}
	if patchNeeded {
		if _, err := r.client.UpdateLoadBalancer(ctx, id, updReq); err != nil {
			resp.Diagnostics.AddError("Failed to update Load Balancer", err.Error())
			return
		}
	}

	// 2. Public IP attach/detach
	if !plan.PublicIPID.Equal(state.PublicIPID) {
		if plan.PublicIPID.IsNull() || plan.PublicIPID.ValueString() == "" {
			if _, err := r.client.DetachLoadBalancerPublicIP(ctx, id); err != nil {
				resp.Diagnostics.AddError("Failed to detach public IP", err.Error())
				return
			}
		} else {
			req := client.LoadBalancerAttachIPRequest{PublicIPID: plan.PublicIPID.ValueString()}
			if _, err := r.client.AttachLoadBalancerPublicIP(ctx, id, req); err != nil {
				resp.Diagnostics.AddError("Failed to attach public IP", err.Error())
				return
			}
		}
	}

	// Re-fetch final state (poll until status=active again, 2 min)
	final, err := pollUntilReady(ctx, r.client, id, 2*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("LB update did not stabilize", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, final)
	for _, d := range diags {
		resp.Diagnostics.AddWarning("Tags conversion warning", d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *lbResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state lbResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteLoadBalancer(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete Load Balancer", err.Error())
		return
	}
}

func (r *lbResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// pollUntilReady polls GetLoadBalancer until status == active or error/deleting,
// or until timeout. Returns the final LB state.
func pollUntilReady(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.LoadBalancer, error) {
	deadline := time.Now().Add(timeout)
	for {
		lb, err := c.GetLoadBalancer(ctx, id)
		if err != nil {
			return nil, err
		}
		switch lb.Status {
		case client.LbStatusActive:
			return lb, nil
		case client.LbStatusError:
			msg := "unknown"
			if lb.ErrorMessage != nil {
				msg = *lb.ErrorMessage
			}
			return lb, fmt.Errorf("LB entered error state: %s", msg)
		}
		if time.Now().After(deadline) {
			return lb, fmt.Errorf("polling timeout (last status: %s)", lb.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
