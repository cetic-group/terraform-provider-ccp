// Package appgwlistener implements the ccp_appgw_listener Terraform
// resource — a hostname served by an Application Gateway with its
// Let's Encrypt certificate.
//
// Listeners are immutable: hostname and custom_domain cannot change in
// place. To rename a hostname or switch between custom_domain modes,
// destroy and recreate the listener.
package appgwlistener

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/appgwvalidators"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*listenerResource)(nil)
	_ resource.ResourceWithConfigure   = (*listenerResource)(nil)
	_ resource.ResourceWithImportState = (*listenerResource)(nil)
)

func New() resource.Resource { return &listenerResource{} }

type listenerResource struct{ client *client.Client }

type listenerResourceModel struct {
	ID                types.String `tfsdk:"id"`
	AppGWID           types.String `tfsdk:"appgw_id"`
	Hostname          types.String `tfsdk:"hostname"`
	CustomDomain      types.Bool   `tfsdk:"custom_domain"`
	AcmeStatus        types.String `tfsdk:"acme_status"`
	AcmeLastRenewalAt types.String `tfsdk:"acme_last_renewal_at"`
	CertPath          types.String `tfsdk:"cert_path"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

func (r *listenerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_appgw_listener"
}

func (r *listenerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a listener (hostname + TLS cert) on a `ccp_application_gateway`. " +
			"Each listener gets its own Let's Encrypt certificate, served via SNI when the client " +
			"requests this hostname.\n\n" +
			"For `custom_domain = true`, the client must already point a CNAME from `hostname` to the " +
			"gateway's auto-generated subdomain before this resource is created — ACME DNS-01 validation " +
			"will otherwise fail.\n\n" +
			"~> **`appgw_id`, `hostname` and `custom_domain` are immutable.** Any change forces a destroy + create.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the listener.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"appgw_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_application_gateway`. **Immutable**.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "Fully-qualified hostname served by this listener (e.g. `api.example.com`). " +
					"Must be a valid RFC 1123 hostname (max 253 chars). **Immutable**.",
				Required: true,
				Validators: []validator.String{
					appgwvalidators.Hostname(),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"custom_domain": schema.BoolAttribute{
				MarkdownDescription: "When `true`, the hostname is a customer-owned domain (CNAME required, " +
					"ACME validation uses DNS-01). When `false` (default), the listener serves an auto-generated " +
					"subdomain under `app.cloud.cetic-group.com` and ACME uses HTTP-01. **Immutable**.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifierRequiresReplace{}},
			},
			"acme_status": schema.StringAttribute{
				MarkdownDescription: "ACME issuance state: `pending` | `issued` | `failed`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"acme_last_renewal_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last successful certificate renewal.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cert_path": schema.StringAttribute{
				MarkdownDescription: "Server-side filesystem path of the live certificate. Informational.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// boolplanmodifierRequiresReplace forces replacement on any change to a
// bool attribute (custom_domain). The framework's stdlib does not ship
// a `RequiresReplace()` for Bool; we provide a tiny implementation.
type boolplanmodifierRequiresReplace struct{}

func (boolplanmodifierRequiresReplace) Description(_ context.Context) string {
	return "Any change to this attribute forces resource replacement."
}
func (m boolplanmodifierRequiresReplace) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}
func (boolplanmodifierRequiresReplace) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		// Create or destroy — never trigger replace.
		return
	}
	if !req.PlanValue.Equal(req.StateValue) {
		resp.RequiresReplace = true
	}
}

func (r *listenerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func applyToModel(l *client.AppGWListener, m *listenerResourceModel) {
	m.ID = types.StringValue(l.ID)
	m.AppGWID = types.StringValue(l.AppGWID)
	m.Hostname = types.StringValue(l.Hostname)
	m.CustomDomain = types.BoolValue(l.CustomDomain)
	m.AcmeStatus = types.StringValue(l.AcmeStatus)
	m.CreatedAt = types.StringValue(l.CreatedAt)
	if l.AcmeLastRenewalAt != nil {
		m.AcmeLastRenewalAt = types.StringValue(*l.AcmeLastRenewalAt)
	} else {
		m.AcmeLastRenewalAt = types.StringNull()
	}
	if l.CertPath != nil {
		m.CertPath = types.StringValue(*l.CertPath)
	} else {
		m.CertPath = types.StringNull()
	}
}

func (r *listenerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan listenerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.AppGWListenerCreateRequest{
		Hostname: plan.Hostname.ValueString(),
	}
	if !plan.CustomDomain.IsNull() && !plan.CustomDomain.IsUnknown() {
		v := plan.CustomDomain.ValueBool()
		createReq.CustomDomain = &v
	}

	created, err := r.client.CreateAppGWListener(ctx, plan.AppGWID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AppGW listener", err.Error())
		return
	}
	applyToModel(created, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *listenerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state listenerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetAppGWListener(ctx, state.AppGWID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read AppGW listener", err.Error())
		return
	}
	applyToModel(got, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: every field is either Computed or carries a
// RequiresReplace plan modifier. The framework will route any user-facing
// diff through destroy + create rather than here.
func (r *listenerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan listenerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *listenerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state listenerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteAppGWListener(ctx, state.AppGWID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete AppGW listener", err.Error())
	}
}

// ImportState accepts `<appgw_id>/<listener_id>`. Splitting on '/' rather
// than a UUID-only id keeps Import symmetric with how the listener is
// retrieved (the API requires both UUIDs).
func (r *listenerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitID(req.ID)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<appgw_id>/<listener_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("appgw_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}

func splitID(id string) []string {
	parts := []string{}
	cur := ""
	for _, ch := range id {
		if ch == '/' {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(ch)
	}
	parts = append(parts, cur)
	return parts
}
