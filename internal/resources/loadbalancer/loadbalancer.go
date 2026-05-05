// Package loadbalancer implements the ccp_load_balancer Terraform resource.
//
// The resource manages the full lifecycle of a load balancer including listeners
// and their backends. Listeners and backends declared in the Terraform config are
// reconciled on every apply — additions and removals are handled automatically.
package loadbalancer

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
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

type lbBackendModel struct {
	ID          types.String `tfsdk:"id"`
	ContainerID types.String `tfsdk:"container_id"`
	VMID        types.String `tfsdk:"vm_instance_id"`
	Port        types.Int64  `tfsdk:"port"`
	Weight      types.Int64  `tfsdk:"weight"`
}

type lbListenerModel struct {
	ID           types.String     `tfsdk:"id"`
	Name         types.String     `tfsdk:"name"`
	Algorithm    types.String     `tfsdk:"algorithm"`
	Protocol     types.String     `tfsdk:"protocol"`
	FrontendPort types.Int64      `tfsdk:"frontend_port"`
	Backends     []lbBackendModel `tfsdk:"backend"`
}

type lbResourceModel struct {
	ID              types.String      `tfsdk:"id"`
	Name            types.String      `tfsdk:"name"`
	Region          types.String      `tfsdk:"region"`
	VnetID          types.String      `tfsdk:"vnet_id"`
	PublicIPID      types.String      `tfsdk:"public_ip_id"`
	VIPAddress      types.String      `tfsdk:"vip_address"`
	PublicIPAddress types.String      `tfsdk:"public_ip_address"`
	Status          types.String      `tfsdk:"status"`
	Tags            types.List        `tfsdk:"tags"`
	Listeners       []lbListenerModel `tfsdk:"listener"`
	CreatedAt       types.String      `tfsdk:"created_at"`
}

func (r *lbResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer"
}

func (r *lbResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud Load Balancer. " +
			"Supports listeners (TCP/HTTP) with weighted backends (container instances or VM instances). " +
			"Supports public IP attachment via `public_ip_id`.",
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
				MarkdownDescription: "Region code (RNN, PAR, ABJ). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet the load balancer's virtual IP is hosted on. Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a `ccp_public_ip` to attach as the public entrypoint. " +
					"Set to attach, remove to detach.",
				Optional: true,
			},
			"vip_address": schema.StringAttribute{
				MarkdownDescription: "Private virtual IP address within the VNet. Available once status is `active`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IP address, if one is attached. Empty otherwise.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Provisioning status: `provisioning` | `active` | `updating` | `error`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form tags (max 60, max 50 chars each).",
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
		Blocks: map[string]schema.Block{
			"listener": schema.ListNestedBlock{
				MarkdownDescription: "Traffic listener. Each listener binds a frontend port and forwards to a set of backends.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Server-assigned UUID of the listener.",
							Computed:            true,
							PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Listener name (1-100 chars).",
							Required:            true,
							Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
						},
						"algorithm": schema.StringAttribute{
							MarkdownDescription: "Load-balancing algorithm: `round_robin`, `least_conn`, `ip_hash`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("round_robin", "least_conn", "ip_hash"),
							},
						},
						"protocol": schema.StringAttribute{
							MarkdownDescription: "Protocol: `tcp` or `http`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("tcp", "http"),
							},
						},
						"frontend_port": schema.Int64Attribute{
							MarkdownDescription: "Port the load balancer listens on (1-65535).",
							Required:            true,
							Validators: []validator.Int64{
								int64validator.Between(1, 65535),
							},
						},
					},
					Blocks: map[string]schema.Block{
						"backend": schema.ListNestedBlock{
							MarkdownDescription: "Backend target. Exactly one of `container_id` or `vm_instance_id` must be set.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"id": schema.StringAttribute{
										MarkdownDescription: "Server-assigned UUID of the backend.",
										Computed:            true,
										PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
									},
									"container_id": schema.StringAttribute{
										MarkdownDescription: "UUID of the container instance to use as a backend.",
										Optional:            true,
									},
									"vm_instance_id": schema.StringAttribute{
										MarkdownDescription: "UUID of the VM instance to use as a backend.",
										Optional:            true,
									},
									"port": schema.Int64Attribute{
										MarkdownDescription: "Backend port (1-65535).",
										Required:            true,
										Validators: []validator.Int64{
											int64validator.Between(1, 65535),
										},
									},
									"weight": schema.Int64Attribute{
										MarkdownDescription: "Backend weight for weighted round-robin (1-100). Defaults to 1.",
										Optional:            true,
										Computed:            true,
										Default:             int64default.StaticInt64(1),
									},
								},
							},
						},
					},
				},
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

	// Map listeners from API
	for _, l := range lb.Listeners {
		lm := lbListenerModel{
			ID:           types.StringValue(l.ID),
			Name:         types.StringValue(l.Name),
			Algorithm:    types.StringValue(l.Algorithm),
			Protocol:     types.StringValue(l.Protocol),
			FrontendPort: types.Int64Value(int64(l.FrontendPort)),
		}
		for _, b := range l.Backends {
			bm := lbBackendModel{
				ID:     types.StringValue(b.ID),
				Port:   types.Int64Value(int64(b.Port)),
				Weight: types.Int64Value(int64(b.Weight)),
			}
			if b.ContainerID != nil {
				bm.ContainerID = types.StringValue(*b.ContainerID)
				bm.VMID = types.StringNull()
			} else if b.VMID != nil {
				bm.VMID = types.StringValue(*b.VMID)
				bm.ContainerID = types.StringNull()
			} else {
				bm.ContainerID = types.StringNull()
				bm.VMID = types.StringNull()
			}
			lm.Backends = append(lm.Backends, bm)
		}
		m.Listeners = append(m.Listeners, lm)
	}

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

	// Poll until active
	final, err := pollUntilReady(ctx, r.client, created.ID, 5*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("LB provisioning timed out or failed", err.Error())
		return
	}

	// Create listeners and their backends
	for i, lPlan := range plan.Listeners {
		lReq := client.LBListenerCreateRequest{
			Name:         lPlan.Name.ValueString(),
			Algorithm:    lPlan.Algorithm.ValueString(),
			Protocol:     lPlan.Protocol.ValueString(),
			FrontendPort: int(lPlan.FrontendPort.ValueInt64()),
		}
		createdL, err := r.client.CreateLBListener(ctx, final.ID, lReq)
		if err != nil {
			resp.Diagnostics.AddError(fmt.Sprintf("Failed to create listener %q", lPlan.Name.ValueString()), err.Error())
			return
		}
		plan.Listeners[i].ID = types.StringValue(createdL.ID)

		for j, bPlan := range lPlan.Backends {
			bReq := client.LBBackendCreateRequest{
				Port:   int(bPlan.Port.ValueInt64()),
				Weight: int(bPlan.Weight.ValueInt64()),
			}
			if !bPlan.ContainerID.IsNull() && !bPlan.ContainerID.IsUnknown() && bPlan.ContainerID.ValueString() != "" {
				v := bPlan.ContainerID.ValueString()
				bReq.ContainerID = &v
			} else if !bPlan.VMID.IsNull() && !bPlan.VMID.IsUnknown() && bPlan.VMID.ValueString() != "" {
				v := bPlan.VMID.ValueString()
				bReq.VMID = &v
			}
			createdB, err := r.client.AddLBBackend(ctx, final.ID, createdL.ID, bReq)
			if err != nil {
				resp.Diagnostics.AddError(fmt.Sprintf("Failed to add backend to listener %q", lPlan.Name.ValueString()), err.Error())
				return
			}
			plan.Listeners[i].Backends[j].ID = types.StringValue(createdB.ID)
		}
	}

	// Re-fetch to get consistent server state
	final, err = r.client.GetLoadBalancer(ctx, final.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to re-read LB after creation", err.Error())
		return
	}

	state, diags := stateFromAPI(ctx, final)
	// Preserve plan listener/backend IDs we just set (server may not return them inline)
	if len(state.Listeners) == 0 && len(plan.Listeners) > 0 {
		state.Listeners = plan.Listeners
	}
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

	// 1. Patch name + tags
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
			ipReq := client.LoadBalancerAttachIPRequest{PublicIPID: plan.PublicIPID.ValueString()}
			if _, err := r.client.AttachLoadBalancerPublicIP(ctx, id, ipReq); err != nil {
				resp.Diagnostics.AddError("Failed to attach public IP", err.Error())
				return
			}
		}
	}

	// 3. Reconcile listeners: remove state listeners not in plan (matched by name)
	planListenersByName := map[string]lbListenerModel{}
	for _, l := range plan.Listeners {
		planListenersByName[l.Name.ValueString()] = l
	}
	stateListenersByName := map[string]lbListenerModel{}
	for _, l := range state.Listeners {
		stateListenersByName[l.Name.ValueString()] = l
	}

	for name, stL := range stateListenersByName {
		if _, ok := planListenersByName[name]; !ok {
			if err := r.client.DeleteLBListener(ctx, id, stL.ID.ValueString()); err != nil && !client.IsNotFound(err) {
				resp.Diagnostics.AddError(fmt.Sprintf("Failed to delete listener %q", name), err.Error())
				return
			}
		}
	}

	// 4. Add plan listeners not in state; for existing ones reconcile backends
	for i, pL := range plan.Listeners {
		name := pL.Name.ValueString()
		if stL, exists := stateListenersByName[name]; exists {
			// Listener exists — carry over its ID and reconcile backends
			plan.Listeners[i].ID = stL.ID
			lID := stL.ID.ValueString()

			planBackendsByKey := map[string]lbBackendModel{}
			for _, b := range pL.Backends {
				planBackendsByKey[backendKey(b)] = b
			}
			stateBackendsByKey := map[string]lbBackendModel{}
			for _, b := range stL.Backends {
				stateBackendsByKey[backendKey(b)] = b
			}

			// Remove backends not in plan
			for key, stB := range stateBackendsByKey {
				if _, ok := planBackendsByKey[key]; !ok {
					if err := r.client.RemoveLBBackend(ctx, id, lID, stB.ID.ValueString()); err != nil && !client.IsNotFound(err) {
						resp.Diagnostics.AddError("Failed to remove backend", err.Error())
						return
					}
				}
			}
			// Add backends not in state
			for j, pB := range pL.Backends {
				key := backendKey(pB)
				if stB, ok := stateBackendsByKey[key]; ok {
					plan.Listeners[i].Backends[j].ID = stB.ID
				} else {
					bReq := backendCreateReq(pB)
					createdB, err := r.client.AddLBBackend(ctx, id, lID, bReq)
					if err != nil {
						resp.Diagnostics.AddError("Failed to add backend", err.Error())
						return
					}
					plan.Listeners[i].Backends[j].ID = types.StringValue(createdB.ID)
				}
			}
		} else {
			// New listener
			lReq := client.LBListenerCreateRequest{
				Name:         pL.Name.ValueString(),
				Algorithm:    pL.Algorithm.ValueString(),
				Protocol:     pL.Protocol.ValueString(),
				FrontendPort: int(pL.FrontendPort.ValueInt64()),
			}
			createdL, err := r.client.CreateLBListener(ctx, id, lReq)
			if err != nil {
				resp.Diagnostics.AddError(fmt.Sprintf("Failed to create listener %q", name), err.Error())
				return
			}
			plan.Listeners[i].ID = types.StringValue(createdL.ID)

			for j, bPlan := range pL.Backends {
				bReq := backendCreateReq(bPlan)
				createdB, err := r.client.AddLBBackend(ctx, id, createdL.ID, bReq)
				if err != nil {
					resp.Diagnostics.AddError("Failed to add backend", err.Error())
					return
				}
				plan.Listeners[i].Backends[j].ID = types.StringValue(createdB.ID)
			}
		}
	}

	// Re-fetch final state
	final, err := pollUntilReady(ctx, r.client, id, 2*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("LB update did not stabilize", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, final)
	if len(newState.Listeners) == 0 && len(plan.Listeners) > 0 {
		newState.Listeners = plan.Listeners
	}
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

// backendKey returns a stable string key for backend deduplication during reconcile.
func backendKey(b lbBackendModel) string {
	target := b.ContainerID.ValueString()
	if target == "" {
		target = "vm:" + b.VMID.ValueString()
	}
	return fmt.Sprintf("%s:%d", target, b.Port.ValueInt64())
}

func backendCreateReq(b lbBackendModel) client.LBBackendCreateRequest {
	req := client.LBBackendCreateRequest{
		Port:   int(b.Port.ValueInt64()),
		Weight: int(b.Weight.ValueInt64()),
	}
	if !b.ContainerID.IsNull() && !b.ContainerID.IsUnknown() && b.ContainerID.ValueString() != "" {
		v := b.ContainerID.ValueString()
		req.ContainerID = &v
	} else if !b.VMID.IsNull() && !b.VMID.IsUnknown() && b.VMID.ValueString() != "" {
		v := b.VMID.ValueString()
		req.VMID = &v
	}
	return req
}

// pollUntilReady polls GetLoadBalancer until status == active or error, or until timeout.
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
