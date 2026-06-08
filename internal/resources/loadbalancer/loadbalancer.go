// Package loadbalancer implements the ccp_load_balancer Terraform resource.
//
// The resource manages the full lifecycle of a load balancer including its
// listeners and their backends. Listeners are sent in the initial POST that
// creates the LB — the backend exposes NO endpoint to add, patch or delete a
// listener afterwards. Any change to an immutable listener field therefore
// forces replacement of the whole LB (see ModifyPlan). Backends, on the other
// hand, can be reconciled in place (add/update/remove) after creation.
package loadbalancer

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*lbResource)(nil)
	_ resource.ResourceWithConfigure   = (*lbResource)(nil)
	_ resource.ResourceWithImportState = (*lbResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*lbResource)(nil)
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
	ID                 types.String     `tfsdk:"id"`
	Protocol           types.String     `tfsdk:"protocol"`
	ListenPort         types.Int64      `tfsdk:"listen_port"`
	Algorithm          types.String     `tfsdk:"algorithm"`
	HealthCheckEnabled types.Bool       `tfsdk:"health_check_enabled"`
	HealthCheckPath    types.String     `tfsdk:"health_check_path"`
	Domain             types.String     `tfsdk:"domain"`
	AcmeChallenge      types.String     `tfsdk:"acme_challenge"`
	AcmeDNSProvider    types.String     `tfsdk:"acme_dns_provider"`
	AcmeDNSCredentials types.Map        `tfsdk:"acme_dns_credentials"`
	AcmeStatus         types.String     `tfsdk:"acme_status"`
	AcmeLastError      types.String     `tfsdk:"acme_last_error"`
	Backends           []lbBackendModel `tfsdk:"backend"`
}

type lbResourceModel struct {
	ID              types.String      `tfsdk:"id"`
	Name            types.String      `tfsdk:"name"`
	Region          types.String      `tfsdk:"region"`
	Plan            types.String      `tfsdk:"plan"`
	VnetID          types.String      `tfsdk:"vnet_id"`
	PublicIPID      types.String      `tfsdk:"public_ip_id"`
	VIPAddress      types.String      `tfsdk:"vip_address"`
	PublicIPAddress types.String      `tfsdk:"public_ip_address"`
	Status          types.String      `tfsdk:"status"`
	Tags            types.List        `tfsdk:"tags"`
	Listeners       []lbListenerModel `tfsdk:"listener"`
	CreatedAt       types.String      `tfsdk:"created_at"`
}

func (r *lbResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_load_balancer"
}

func (r *lbResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud Load Balancer. " +
			"Listeners (TCP/HTTP/HTTPS) with weighted backends (container or VM instances) " +
			"are declared in the resource and sent at creation time. HTTPS listeners can " +
			"obtain a Let's Encrypt certificate automatically via ACME (`http01`/`dns01`). " +
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
			"plan": schema.StringAttribute{
				MarkdownDescription: "Capacity plan: `small` (default), `medium` or `large`. " +
					"Changing the plan forces replacement.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("small"),
				Validators: []validator.String{
					stringvalidator.OneOf("small", "medium", "large"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
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
				// No UseStateForUnknown: status is volatile (transitions to
				// `updating` async on apply) — must stay known-after-apply.
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
				MarkdownDescription: "Traffic listener. Listeners are sent in the initial create request — " +
					"changing an immutable listener field (protocol, listen port, algorithm, health check, " +
					"domain or ACME settings) forces replacement of the whole load balancer. Backends can be " +
					"reconciled in place.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Server-assigned UUID of the listener.",
							Computed:            true,
							PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						},
						"protocol": schema.StringAttribute{
							MarkdownDescription: "Protocol: `tcp`, `http` or `https`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("tcp", "http", "https"),
							},
						},
						"listen_port": schema.Int64Attribute{
							MarkdownDescription: "Port the load balancer listens on (1-65535).",
							Required:            true,
							Validators: []validator.Int64{
								int64validator.Between(1, 65535),
							},
						},
						"algorithm": schema.StringAttribute{
							MarkdownDescription: "Load-balancing algorithm: `roundrobin` (default), `leastconn`, `source` or `random`.",
							Optional:            true,
							Computed:            true,
							Default:             stringdefault.StaticString("roundrobin"),
							Validators: []validator.String{
								stringvalidator.OneOf("roundrobin", "leastconn", "source", "random"),
							},
						},
						"health_check_enabled": schema.BoolAttribute{
							MarkdownDescription: "Enable backend health checks. Defaults to true.",
							Optional:            true,
							Computed:            true,
							Default:             booldefault.StaticBool(true),
						},
						"health_check_path": schema.StringAttribute{
							MarkdownDescription: "HTTP path used for health checks (for `http`/`https` listeners).",
							Optional:            true,
						},
						"domain": schema.StringAttribute{
							MarkdownDescription: "Domain name served by an `https` listener. Required when ACME is enabled. " +
								"Must be a lowercase fully-qualified domain name — the backend lowercases and strips " +
								"input, so any other form would drift from the stored value.",
							Optional: true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`),
									"must be a lowercase fully-qualified domain name",
								),
							},
						},
						"acme_challenge": schema.StringAttribute{
							MarkdownDescription: "ACME (Let's Encrypt) challenge type: `http01` or `dns01`. " +
								"Requires `protocol = \"https\"` and `domain`. `dns01` additionally requires " +
								"`acme_dns_provider` + `acme_dns_credentials`.",
							Optional: true,
							Validators: []validator.String{
								stringvalidator.OneOf("http01", "dns01"),
							},
						},
						"acme_dns_provider": schema.StringAttribute{
							MarkdownDescription: "DNS-01 provider id (e.g. `cloudflare`, `route53`, `ionos`). " +
								"See the `ccp_acme_dns_providers` data source for the supported list. " +
								"Required for `acme_challenge = \"dns01\"`.",
							Optional: true,
						},
						"acme_dns_credentials": schema.MapAttribute{
							MarkdownDescription: "DNS provider credentials for `dns01` (write-only — never returned by the API). " +
								"Keys depend on the provider (see `ccp_acme_dns_providers`).",
							ElementType: types.StringType,
							Optional:    true,
							Sensitive:   true,
						},
						"acme_status": schema.StringAttribute{
							MarkdownDescription: "ACME certificate status: `pending` | `issuing` | `issued` | `renewing` | `error`.",
							Computed:            true,
							// No UseStateForUnknown: ACME status is volatile.
						},
						"acme_last_error": schema.StringAttribute{
							MarkdownDescription: "Last certificate issuance error, if any. Cleared when issuance succeeds.",
							Computed:            true,
							// No UseStateForUnknown: volatile, like acme_status.
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
										MarkdownDescription: "Backend weight (0-256). Defaults to 1. Weight changes are reconciled in place.",
										Optional:            true,
										Computed:            true,
										Default:             int64default.StaticInt64(1),
										Validators: []validator.Int64{
											int64validator.Between(0, 256),
										},
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

// ModifyPlan forces replacement of the whole LB when an immutable listener
// field changes. Listeners cannot be added/removed/patched after creation
// (no backend endpoint), so the only safe reconciliation is destroy+create.
// Backend blocks and Computed-only fields are excluded — they reconcile in
// place in Update(). Per the framework rules, ModifyPlan only appends to
// RequiresReplace; it never rewrites Required attribute values.
func (r *lbResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Create (no prior state) or destroy (no plan) — nothing to compare.
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state lbResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if listenersRequireReplace(plan.Listeners, state.Listeners) {
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("listener"))
	}
}

// listenersRequireReplace returns true when the set of listeners differs on any
// immutable field (count, protocol, listen_port, algorithm, health check,
// domain, ACME settings). Backend blocks and weight are NOT considered — those
// reconcile in place. ACME credentials are write-only and never compared.
// Listeners are matched by (protocol, listen_port).
func listenersRequireReplace(plan, state []lbListenerModel) bool {
	if len(plan) != len(state) {
		return true
	}
	stateByKey := make(map[string]lbListenerModel, len(state))
	for _, l := range state {
		stateByKey[listenerKey(l)] = l
	}
	for _, p := range plan {
		s, ok := stateByKey[listenerKey(p)]
		if !ok {
			// A listener with a new (protocol, listen_port) pair appeared.
			return true
		}
		if listenerImmutableChanged(p, s) {
			return true
		}
	}
	return false
}

// listenerKey is the identity used to match plan↔state/API listeners.
// (protocol, listen_port) is unique de facto.
func listenerKey(l lbListenerModel) string {
	return fmt.Sprintf("%s:%d", l.Protocol.ValueString(), l.ListenPort.ValueInt64())
}

// listenerImmutableChanged compares two listeners (already matched by key) on
// the fields that cannot be mutated after creation.
func listenerImmutableChanged(a, b lbListenerModel) bool {
	if !a.Protocol.Equal(b.Protocol) ||
		!a.ListenPort.Equal(b.ListenPort) ||
		!a.Algorithm.Equal(b.Algorithm) ||
		!a.HealthCheckEnabled.Equal(b.HealthCheckEnabled) ||
		!a.HealthCheckPath.Equal(b.HealthCheckPath) ||
		!a.Domain.Equal(b.Domain) ||
		!a.AcmeChallenge.Equal(b.AcmeChallenge) ||
		!a.AcmeDNSProvider.Equal(b.AcmeDNSProvider) {
		return true
	}
	return false
}

// stateFromAPI maps the API load balancer onto the Terraform model. Listeners
// from the API are matched to the prior plan/state listeners by (protocol,
// listen_port) so we can carry over write-only credentials (never returned by
// the API) and avoid a perma-diff. `prior` may be nil (e.g. on import).
func stateFromAPI(ctx context.Context, lb *client.LoadBalancer, prior []lbListenerModel) (lbResourceModel, []string) {
	m := lbResourceModel{
		ID:        types.StringValue(lb.ID),
		Name:      types.StringValue(lb.Name),
		Region:    types.StringValue(lb.Region),
		Plan:      types.StringValue(lb.Plan),
		VnetID:    types.StringValue(lb.VnetID),
		Status:    types.StringValue(lb.Status),
		CreatedAt: types.StringValue(lb.CreatedAt),
	}
	setOptStr(&m.PublicIPID, lb.PublicIPID)
	setOptStr(&m.VIPAddress, lb.VIPAddress)
	setOptStr(&m.PublicIPAddress, lb.PublicIPAddress)

	tags, diag := types.ListValueFrom(ctx, types.StringType, lb.Tags)
	var diagStrs []string
	if diag.HasError() {
		for _, d := range diag.Errors() {
			diagStrs = append(diagStrs, d.Summary()+": "+d.Detail())
		}
	}
	m.Tags = tags

	priorByKey := make(map[string]lbListenerModel, len(prior))
	for _, l := range prior {
		priorByKey[listenerKey(l)] = l
	}

	for _, l := range lb.Listeners {
		lm := lbListenerModel{
			ID:                 types.StringValue(l.ID),
			Protocol:           types.StringValue(l.Protocol),
			ListenPort:         types.Int64Value(int64(l.ListenPort)),
			Algorithm:          types.StringValue(l.Algorithm),
			HealthCheckEnabled: types.BoolValue(l.HealthCheckEnabled),
		}
		setOptStr(&lm.HealthCheckPath, l.HealthCheckPath)
		setOptStr(&lm.Domain, l.Domain)
		setOptStr(&lm.AcmeChallenge, l.AcmeChallenge)
		setOptStr(&lm.AcmeDNSProvider, l.AcmeDNSProvider)
		setOptStr(&lm.AcmeStatus, l.AcmeStatus)
		setOptStr(&lm.AcmeLastError, l.AcmeLastError)

		// acme_dns_credentials is write-only — carry over the prior value
		// (never mark Unknown or Null, otherwise perma-diff).
		key := fmt.Sprintf("%s:%d", l.Protocol, l.ListenPort)
		if p, ok := priorByKey[key]; ok {
			lm.AcmeDNSCredentials = p.AcmeDNSCredentials
		} else {
			lm.AcmeDNSCredentials = types.MapNull(types.StringType)
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

func setOptStr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}

// listenerCreateReq builds the API create request for a listener from the plan.
func listenerCreateReq(ctx context.Context, l lbListenerModel) client.LBListenerCreateRequest {
	req := client.LBListenerCreateRequest{
		Protocol:   l.Protocol.ValueString(),
		ListenPort: int(l.ListenPort.ValueInt64()),
	}
	if !l.Algorithm.IsNull() && !l.Algorithm.IsUnknown() && l.Algorithm.ValueString() != "" {
		req.Algorithm = l.Algorithm.ValueString()
	}
	if !l.HealthCheckEnabled.IsNull() && !l.HealthCheckEnabled.IsUnknown() {
		v := l.HealthCheckEnabled.ValueBool()
		req.HealthCheckEnabled = &v
	}
	if !l.HealthCheckPath.IsNull() && !l.HealthCheckPath.IsUnknown() && l.HealthCheckPath.ValueString() != "" {
		v := l.HealthCheckPath.ValueString()
		req.HealthCheckPath = &v
	}
	if !l.Domain.IsNull() && !l.Domain.IsUnknown() && l.Domain.ValueString() != "" {
		v := l.Domain.ValueString()
		req.Domain = &v
	}
	if !l.AcmeChallenge.IsNull() && !l.AcmeChallenge.IsUnknown() && l.AcmeChallenge.ValueString() != "" {
		v := l.AcmeChallenge.ValueString()
		req.AcmeChallenge = &v
	}
	if !l.AcmeDNSProvider.IsNull() && !l.AcmeDNSProvider.IsUnknown() && l.AcmeDNSProvider.ValueString() != "" {
		v := l.AcmeDNSProvider.ValueString()
		req.AcmeDNSProvider = &v
	}
	if !l.AcmeDNSCredentials.IsNull() && !l.AcmeDNSCredentials.IsUnknown() {
		creds := map[string]string{}
		l.AcmeDNSCredentials.ElementsAs(ctx, &creds, false)
		if len(creds) > 0 {
			req.AcmeDNSCredentials = creds
		}
	}
	for _, b := range l.Backends {
		req.Backends = append(req.Backends, backendCreateReq(b))
	}
	return req
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
	if !plan.Plan.IsNull() && !plan.Plan.IsUnknown() && plan.Plan.ValueString() != "" {
		createReq.Plan = plan.Plan.ValueString()
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
	// Listeners (with their backends) are created in this single POST.
	for _, l := range plan.Listeners {
		createReq.Listeners = append(createReq.Listeners, listenerCreateReq(ctx, l))
	}

	created, err := r.client.CreateLoadBalancer(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Load Balancer", err.Error())
		return
	}

	final, err := pollUntilReady(ctx, r.client, created.ID, 5*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("LB provisioning timed out or failed", err.Error())
		return
	}

	state, diags := stateFromAPI(ctx, final, plan.Listeners)
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
	newState, diags := stateFromAPI(ctx, got, state.Listeners)
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

	// 1. Patch name + tags.
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

	// 2. Public IP attach/detach.
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

	// 3. Reconcile backends per listener. Listeners themselves are immutable
	//    (ModifyPlan forces replacement on any change), so we match plan↔state
	//    listeners by (protocol, listen_port) and only touch their backends.
	stateListenersByKey := map[string]lbListenerModel{}
	for _, l := range state.Listeners {
		stateListenersByKey[listenerKey(l)] = l
	}

	for _, pL := range plan.Listeners {
		stL, exists := stateListenersByKey[listenerKey(pL)]
		if !exists {
			// Should be unreachable: a changed listener key triggers replace.
			continue
		}
		lID := stL.ID.ValueString()

		planBackendsByKey := map[string]lbBackendModel{}
		for _, b := range pL.Backends {
			planBackendsByKey[backendKey(b)] = b
		}
		stateBackendsByKey := map[string]lbBackendModel{}
		for _, b := range stL.Backends {
			stateBackendsByKey[backendKey(b)] = b
		}

		// Remove backends present in state but not in plan.
		for key, stB := range stateBackendsByKey {
			if _, ok := planBackendsByKey[key]; !ok {
				if err := r.client.RemoveLBBackend(ctx, id, lID, stB.ID.ValueString()); err != nil && !client.IsNotFound(err) {
					resp.Diagnostics.AddError("Failed to remove backend", err.Error())
					return
				}
			}
		}
		// Add new backends; reconcile weight on existing ones via UpdateLBBackend.
		for _, pB := range pL.Backends {
			key := backendKey(pB)
			if stB, ok := stateBackendsByKey[key]; ok {
				if !pB.Weight.Equal(stB.Weight) {
					w := int(pB.Weight.ValueInt64())
					if _, err := r.client.UpdateLBBackend(ctx, id, lID, stB.ID.ValueString(), client.LBBackendUpdateRequest{Weight: &w}); err != nil {
						resp.Diagnostics.AddError("Failed to update backend weight", err.Error())
						return
					}
				}
			} else {
				if _, err := r.client.AddLBBackend(ctx, id, lID, backendCreateReq(pB)); err != nil {
					resp.Diagnostics.AddError("Failed to add backend", err.Error())
					return
				}
			}
		}
	}

	// Re-fetch final state.
	final, err := pollUntilReady(ctx, r.client, id, 2*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("LB update did not stabilize", err.Error())
		return
	}
	newState, diags := stateFromAPI(ctx, final, plan.Listeners)
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
	if err := client.PollUntilDeleted(ctx, 20*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetLoadBalancer(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm Load Balancer deletion", err.Error())
	}
}

func (r *lbResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// backendKey returns a stable string key for backend deduplication during
// reconcile. The identity is (target, port) where target is the container or
// VM id — a single backend cannot point to both (XOR enforced server-side).
func backendKey(b lbBackendModel) string {
	target := b.ContainerID.ValueString()
	if target == "" {
		target = "vm:" + b.VMID.ValueString()
	} else {
		target = "ct:" + target
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
