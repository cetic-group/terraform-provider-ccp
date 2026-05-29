// Package applicationgateway implements the ccp_application_gateway
// Terraform resource — the L7 Application Gateway (ccp-appgw) which
// provisions an HA pair of LXC containers running HAProxy with TLS
// termination, SNI multi-cert, rate limiting and WAF presets.
//
// Listeners, target groups, target group members and routes are managed
// as SEPARATE resources (ccp_appgw_listener, ccp_appgw_target_group,
// ccp_appgw_target_group_member, ccp_appgw_route). That decoupling makes
// per-route HCL edits idempotent without re-applying the whole gateway.
//
// region / vpc_id / vnet_id are immutable: changing them forces a
// destroy+create. plan / public_ip_id / force_https / hsts_* / global_*
// can be patched in place.
package applicationgateway

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/appgwvalidators"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*appgwResource)(nil)
	_ resource.ResourceWithConfigure   = (*appgwResource)(nil)
	_ resource.ResourceWithImportState = (*appgwResource)(nil)
)

func New() resource.Resource { return &appgwResource{} }

type appgwResource struct{ client *client.Client }

type appgwResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Region                types.String `tfsdk:"region"`
	Plan                  types.String `tfsdk:"plan"`
	VpcID                 types.String `tfsdk:"vpc_id"`
	VnetID                types.String `tfsdk:"vnet_id"`
	PublicIPID            types.String `tfsdk:"public_ip_id"`
	PublicIPAddress       types.String `tfsdk:"public_ip_address"`
	PublicIPStatus        types.String `tfsdk:"public_ip_status"`
	VIPAddress            types.String `tfsdk:"vip_address"`
	Status                types.String `tfsdk:"status"`
	ErrorMessage          types.String `tfsdk:"error_message"`
	ForceHTTPS            types.Bool   `tfsdk:"force_https"`
	HSTSEnabled           types.Bool   `tfsdk:"hsts_enabled"`
	HSTSMaxAge            types.Int64  `tfsdk:"hsts_max_age"`
	GlobalRateLimitPerSec types.Int64  `tfsdk:"global_rate_limit_per_sec"`
	GlobalAllowCIDRs      types.List   `tfsdk:"global_allow_cidrs"`
	GlobalDenyCIDRs       types.List   `tfsdk:"global_deny_cidrs"`
	TrustProxyHeaders     types.Bool   `tfsdk:"trust_proxy_headers"`
	Tags                  types.List   `tfsdk:"tags"`
	CreatedAt             types.String `tfsdk:"created_at"`
}

func (r *appgwResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_application_gateway"
}

func (r *appgwResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud Application Gateway (ccp-appgw) — an L7 HTTP/HTTPS " +
			"reverse proxy with TLS termination, SNI multi-cert, rate limiting, IP allow/deny lists, " +
			"CORS, basic auth and WAF presets.\n\n" +
			"Each gateway provisions a highly-available pair of containers behind a floating virtual IP. " +
			"Listeners, routes and target groups are declared as separate resources " +
			"(`ccp_appgw_listener`, `ccp_appgw_target_group`, `ccp_appgw_route`, " +
			"`ccp_appgw_target_group_member`) to keep per-route HCL edits idempotent.\n\n" +
			"~> **Provisioning is asynchronous.** The provider polls until status is `active` " +
			"(typically 3-5 minutes for the initial create).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the gateway.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (1-100 chars).",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code (e.g. `RNN`, `PAR`, `ABJ`). **Immutable** — forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Capacity plan: `small` (50 routes / 100 req/s), " +
					"`medium` (200 routes / 1000 req/s) or `large` (1000 routes / 10000 req/s).",
				Required:   true,
				Validators: []validator.String{stringvalidator.OneOf(appgwvalidators.AppGWPlans...)},
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VPC the gateway is provisioned in. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VNet the gateway's VIP is hosted on. Backends declared via " +
					"target group members must be reachable from this VNet. **Immutable**.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a `ccp_public_ip` to attach as the public entrypoint. " +
					"Set to attach, remove to detach. Attach/detach is asynchronous — the provider " +
					"polls until `public_ip_status` stabilises.",
				Optional: true,
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IPv4 address currently bound to the gateway, mirrored from " +
					"the attached `ccp_public_ip`. Null while no IP is attached.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"public_ip_status": schema.StringAttribute{
				MarkdownDescription: "Lifecycle of the public IP attachment: `allocated` | `attaching` | " +
					"`attached` | `detaching` | `error`. Null when no IP is attached. Mirrors the same " +
					"helper exposed by `ccp_load_balancer`, `ccp_vm_instance` and `ccp_container_instance` " +
					"(see the platform `public_ip` UX convention, 2026-05-02).",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"vip_address": schema.StringAttribute{
				MarkdownDescription: "Private virtual IP address within the VNet. Available once status is `active`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Provisioning status: `creating` | `active` | `error` | `deleting`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"error_message": schema.StringAttribute{
				MarkdownDescription: "Last error message reported by the provisioner. Empty unless status is `error`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"force_https": schema.BoolAttribute{
				MarkdownDescription: "Redirect plain HTTP (`:80`) to HTTPS (`:443`). Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"hsts_enabled": schema.BoolAttribute{
				MarkdownDescription: "Set the `Strict-Transport-Security` header on every HTTPS response. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"hsts_max_age": schema.Int64Attribute{
				MarkdownDescription: "`max-age` directive of the HSTS header in seconds. Defaults to `31536000` (1 year).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(31536000),
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
			"global_rate_limit_per_sec": schema.Int64Attribute{
				MarkdownDescription: "Gateway-wide rate limit in req/sec/IP. Null disables the global limit " +
					"(routes can still set their own).",
				Optional:   true,
				Validators: []validator.Int64{int64validator.AtLeast(0)},
			},
			"global_allow_cidrs": schema.ListAttribute{
				MarkdownDescription: "List of CIDRs allowed to hit any route on the gateway. Empty = allow all.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.List{appgwvalidators.CIDRList()},
			},
			"global_deny_cidrs": schema.ListAttribute{
				MarkdownDescription: "List of CIDRs denied access to the gateway. Evaluated before allow.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators:          []validator.List{appgwvalidators.CIDRList()},
			},
			"trust_proxy_headers": schema.BoolAttribute{
				MarkdownDescription: "Accept incoming `X-Forwarded-For` / `X-Real-IP` headers from clients (useful when " +
					"the gateway is fronted by another reverse proxy or CDN). Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
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
	}
}

func (r *appgwResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// applyToModel maps an API ApplicationGateway onto the Terraform model.
// CALLERS must preserve any plan-side intent (e.g. tags lists that the
// API normalised) BEFORE invoking this helper — see CLAUDE.md pitfall #3.
func applyToModel(ctx context.Context, gw *client.ApplicationGateway, m *appgwResourceModel) []string {
	m.ID = types.StringValue(gw.ID)
	m.Name = types.StringValue(gw.Name)
	m.Region = types.StringValue(gw.Region)
	m.Plan = types.StringValue(gw.Plan)
	m.VpcID = types.StringValue(gw.VpcID)
	m.VnetID = types.StringValue(gw.VnetID)
	m.Status = types.StringValue(gw.Status)
	m.CreatedAt = types.StringValue(gw.CreatedAt)
	m.ForceHTTPS = types.BoolValue(gw.ForceHTTPS)
	m.HSTSEnabled = types.BoolValue(gw.HSTSEnabled)
	m.HSTSMaxAge = types.Int64Value(gw.HSTSMaxAge)
	m.TrustProxyHeaders = types.BoolValue(gw.TrustProxyHeaders)

	if gw.PublicIPID != nil {
		m.PublicIPID = types.StringValue(*gw.PublicIPID)
	} else {
		m.PublicIPID = types.StringNull()
	}
	if gw.PublicIPAddress != nil {
		m.PublicIPAddress = types.StringValue(*gw.PublicIPAddress)
	} else {
		m.PublicIPAddress = types.StringNull()
	}
	if gw.PublicIPStatus != nil {
		m.PublicIPStatus = types.StringValue(*gw.PublicIPStatus)
	} else {
		m.PublicIPStatus = types.StringNull()
	}
	if gw.VIPAddress != nil {
		m.VIPAddress = types.StringValue(*gw.VIPAddress)
	} else {
		m.VIPAddress = types.StringNull()
	}
	if gw.ErrorMessage != nil {
		m.ErrorMessage = types.StringValue(*gw.ErrorMessage)
	} else {
		m.ErrorMessage = types.StringNull()
	}
	if gw.GlobalRateLimitPerSec != nil {
		m.GlobalRateLimitPerSec = types.Int64Value(*gw.GlobalRateLimitPerSec)
	} else {
		m.GlobalRateLimitPerSec = types.Int64Null()
	}

	var warnings []string
	allow, d := types.ListValueFrom(ctx, types.StringType, gw.GlobalAllowCIDRs)
	if d.HasError() {
		for _, e := range d.Errors() {
			warnings = append(warnings, e.Summary()+": "+e.Detail())
		}
	}
	m.GlobalAllowCIDRs = allow

	deny, d := types.ListValueFrom(ctx, types.StringType, gw.GlobalDenyCIDRs)
	if d.HasError() {
		for _, e := range d.Errors() {
			warnings = append(warnings, e.Summary()+": "+e.Detail())
		}
	}
	m.GlobalDenyCIDRs = deny

	tags, d := types.ListValueFrom(ctx, types.StringType, gw.Tags)
	if d.HasError() {
		for _, e := range d.Errors() {
			warnings = append(warnings, e.Summary()+": "+e.Detail())
		}
	}
	m.Tags = tags
	return warnings
}

func stringsFromList(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(l.Elements()))
	l.ElementsAs(ctx, &out, false)
	return out
}

func (r *appgwResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appgwResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.ApplicationGatewayCreateRequest{
		Name:   plan.Name.ValueString(),
		Region: plan.Region.ValueString(),
		Plan:   plan.Plan.ValueString(),
		VpcID:  plan.VpcID.ValueString(),
		VnetID: plan.VnetID.ValueString(),
	}
	if !plan.PublicIPID.IsNull() && !plan.PublicIPID.IsUnknown() && plan.PublicIPID.ValueString() != "" {
		v := plan.PublicIPID.ValueString()
		createReq.PublicIPID = &v
	}
	if !plan.ForceHTTPS.IsNull() && !plan.ForceHTTPS.IsUnknown() {
		v := plan.ForceHTTPS.ValueBool()
		createReq.ForceHTTPS = &v
	}
	if !plan.HSTSEnabled.IsNull() && !plan.HSTSEnabled.IsUnknown() {
		v := plan.HSTSEnabled.ValueBool()
		createReq.HSTSEnabled = &v
	}
	if !plan.HSTSMaxAge.IsNull() && !plan.HSTSMaxAge.IsUnknown() {
		v := plan.HSTSMaxAge.ValueInt64()
		createReq.HSTSMaxAge = &v
	}
	if !plan.GlobalRateLimitPerSec.IsNull() && !plan.GlobalRateLimitPerSec.IsUnknown() {
		v := plan.GlobalRateLimitPerSec.ValueInt64()
		createReq.GlobalRateLimitPerSec = &v
	}
	if !plan.TrustProxyHeaders.IsNull() && !plan.TrustProxyHeaders.IsUnknown() {
		v := plan.TrustProxyHeaders.ValueBool()
		createReq.TrustProxyHeaders = &v
	}
	createReq.GlobalAllowCIDRs = stringsFromList(ctx, plan.GlobalAllowCIDRs)
	createReq.GlobalDenyCIDRs = stringsFromList(ctx, plan.GlobalDenyCIDRs)
	createReq.Tags = stringsFromList(ctx, plan.Tags)

	created, err := r.client.CreateApplicationGateway(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Application Gateway", err.Error())
		return
	}

	final, err := pollUntilReady(ctx, r.client, created.ID, 10*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("Application Gateway provisioning failed", err.Error())
		return
	}

	warnings := applyToModel(ctx, final, &plan)
	for _, w := range warnings {
		resp.Diagnostics.AddWarning("State conversion warning", w)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *appgwResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appgwResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetApplicationGateway(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read Application Gateway", err.Error())
		return
	}
	warnings := applyToModel(ctx, got, &state)
	for _, w := range warnings {
		resp.Diagnostics.AddWarning("State conversion warning", w)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *appgwResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state appgwResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// 1. PATCH mutable fields
	var upd client.ApplicationGatewayUpdateRequest
	patchNeeded := false
	if !plan.Name.Equal(state.Name) {
		v := plan.Name.ValueString()
		upd.Name = &v
		patchNeeded = true
	}
	if !plan.ForceHTTPS.Equal(state.ForceHTTPS) {
		v := plan.ForceHTTPS.ValueBool()
		upd.ForceHTTPS = &v
		patchNeeded = true
	}
	if !plan.HSTSEnabled.Equal(state.HSTSEnabled) {
		v := plan.HSTSEnabled.ValueBool()
		upd.HSTSEnabled = &v
		patchNeeded = true
	}
	if !plan.HSTSMaxAge.Equal(state.HSTSMaxAge) {
		v := plan.HSTSMaxAge.ValueInt64()
		upd.HSTSMaxAge = &v
		patchNeeded = true
	}
	if !plan.GlobalRateLimitPerSec.Equal(state.GlobalRateLimitPerSec) {
		if plan.GlobalRateLimitPerSec.IsNull() {
			zero := int64(0)
			// Use 0 to mean "unset" client-side; API treats 0 same as null.
			upd.GlobalRateLimitPerSec = &zero
		} else {
			v := plan.GlobalRateLimitPerSec.ValueInt64()
			upd.GlobalRateLimitPerSec = &v
		}
		patchNeeded = true
	}
	if !plan.GlobalAllowCIDRs.Equal(state.GlobalAllowCIDRs) {
		v := stringsFromList(ctx, plan.GlobalAllowCIDRs)
		if v == nil {
			v = []string{}
		}
		upd.GlobalAllowCIDRs = &v
		patchNeeded = true
	}
	if !plan.GlobalDenyCIDRs.Equal(state.GlobalDenyCIDRs) {
		v := stringsFromList(ctx, plan.GlobalDenyCIDRs)
		if v == nil {
			v = []string{}
		}
		upd.GlobalDenyCIDRs = &v
		patchNeeded = true
	}
	if !plan.TrustProxyHeaders.Equal(state.TrustProxyHeaders) {
		v := plan.TrustProxyHeaders.ValueBool()
		upd.TrustProxyHeaders = &v
		patchNeeded = true
	}
	if !plan.Tags.Equal(state.Tags) {
		v := stringsFromList(ctx, plan.Tags)
		if v == nil {
			v = []string{}
		}
		upd.Tags = &v
		patchNeeded = true
	}
	if patchNeeded {
		if _, err := r.client.UpdateApplicationGateway(ctx, id, upd); err != nil {
			resp.Diagnostics.AddError("Failed to update Application Gateway", err.Error())
			return
		}
	}

	// 2. Public IP attach / detach
	if !plan.PublicIPID.Equal(state.PublicIPID) {
		if plan.PublicIPID.IsNull() || plan.PublicIPID.ValueString() == "" {
			if _, err := r.client.DetachApplicationGatewayPublicIP(ctx, id); err != nil {
				resp.Diagnostics.AddError("Failed to detach public IP", err.Error())
				return
			}
		} else {
			ipReq := client.ApplicationGatewayAttachIPRequest{PublicIPID: plan.PublicIPID.ValueString()}
			if _, err := r.client.AttachApplicationGatewayPublicIP(ctx, id, ipReq); err != nil {
				resp.Diagnostics.AddError("Failed to attach public IP", err.Error())
				return
			}
		}
	}

	final, err := pollUntilReady(ctx, r.client, id, 5*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("Application Gateway update did not stabilize", err.Error())
		return
	}
	warnings := applyToModel(ctx, final, &plan)
	for _, w := range warnings {
		resp.Diagnostics.AddWarning("State conversion warning", w)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *appgwResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appgwResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteApplicationGateway(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete Application Gateway", err.Error())
		return
	}
	if err := client.PollUntilDeleted(ctx, 20*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetApplicationGateway(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm Application Gateway deletion", err.Error())
	}
}

func (r *appgwResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// pollUntilReady polls GetApplicationGateway until status == active, or
// stops on error/deleting. The backend only ever surfaces creating →
// active → error → deleting (no `updating` since v1.8.x — patches are
// applied in-place and the row stays `active` throughout).
func pollUntilReady(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.ApplicationGateway, error) {
	deadline := time.Now().Add(timeout)
	for {
		gw, err := c.GetApplicationGateway(ctx, id)
		if err != nil {
			return nil, err
		}
		switch gw.Status {
		case client.AppGWStatusActive:
			return gw, nil
		case client.AppGWStatusError:
			msg := "unknown"
			if gw.ErrorMessage != nil {
				msg = *gw.ErrorMessage
			}
			return gw, fmt.Errorf("application gateway entered error state: %s", msg)
		}
		if time.Now().After(deadline) {
			return gw, fmt.Errorf("polling timeout (last status: %s)", gw.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
