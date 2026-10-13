// Package appgwlistener implements the ccp_appgw_listener Terraform
// resource — a hostname served by an Application Gateway with its
// optional Let's Encrypt certificate.
//
// Listeners are immutable except for the client-provided certificate
// (#1579): every other user-facing attribute carries a RequiresReplace plan
// modifier. Changing `tls_certificate_pem` / `tls_private_key_pem` rotates
// the certificate IN PLACE (POST .../renew-cert with the new PEMs) —
// recreating the listener would cut the traffic for its hostname.
package appgwlistener

import (
	"context"
	"fmt"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/appgwvalidators"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*listenerResource)(nil)
	_ resource.ResourceWithConfigure      = (*listenerResource)(nil)
	_ resource.ResourceWithImportState    = (*listenerResource)(nil)
	_ resource.ResourceWithValidateConfig = (*listenerResource)(nil)
)

func New() resource.Resource { return &listenerResource{} }

type listenerResource struct{ client *client.Client }

type listenerResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	AppGWID            types.String `tfsdk:"appgw_id"`
	Hostname           types.String `tfsdk:"hostname"`
	AcmeChallenge      types.String `tfsdk:"acme_challenge"`
	AcmeDNSProvider    types.String `tfsdk:"acme_dns_provider"`
	AcmeDNSCredentials types.Map    `tfsdk:"acme_dns_credentials"`
	AcmeStatus         types.String `tfsdk:"acme_status"`
	AcmeLastRenewalAt  types.String `tfsdk:"acme_last_renewal_at"`
	AcmeIssuedAt       types.String `tfsdk:"acme_issued_at"`
	AcmeRenewAfter     types.String `tfsdk:"acme_renew_after"`
	AcmeLastError      types.String `tfsdk:"acme_last_error"`
	CertPath           types.String `tfsdk:"cert_path"`
	TLSCertificatePEM  types.String `tfsdk:"tls_certificate_pem"`
	TLSPrivateKeyPEM   types.String `tfsdk:"tls_private_key_pem"`
	TLSCertNotAfter    types.String `tfsdk:"tls_cert_not_after"`
	TLSCertNames       types.String `tfsdk:"tls_cert_names"`
	TLSCertIssuer      types.String `tfsdk:"tls_cert_issuer"`
	CreatedAt          types.String `tfsdk:"created_at"`
}

func (r *listenerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_appgw_listener"
}

func (r *listenerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a listener (a hostname served by a `ccp_application_gateway`, with an " +
			"optional automatically-issued TLS certificate via Let's Encrypt/ACME). When `acme_challenge` " +
			"is set, the certificate is requested automatically and served over SNI when a client connects " +
			"with this hostname.\n\n" +
			"~> **All attributes except the client certificate are immutable.** Any other change forces a " +
			"destroy + create. A change of `tls_certificate_pem` / `tls_private_key_pem` rotates the " +
			"certificate in place.",
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
					"Must be a valid, lowercase RFC 1123 hostname (max 253 chars). **Immutable**.",
				Required: true,
				Validators: []validator.String{
					appgwvalidators.Hostname(),
					stringvalidator.RegexMatches(lowercaseRe, "hostname must be lowercase"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"acme_challenge": schema.StringAttribute{
				MarkdownDescription: "How the listener's TLS certificate is obtained: `http01` or `dns01` " +
					"(Let's Encrypt; `dns01` additionally requires `acme_dns_provider` and " +
					"`acme_dns_credentials`), or `custom` (your own certificate, provided in " +
					"`tls_certificate_pem` + `tls_private_key_pem`). **Without this attribute, no TLS " +
					"certificate is ever issued for the listener.** **Immutable**.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("http01", "dns01", "custom"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"acme_dns_provider": schema.StringAttribute{
				MarkdownDescription: "DNS provider key used for the `dns01` challenge. See the " +
					"`ccp_acme_dns_providers` data source for the supported catalog. " +
					"Required when `acme_challenge = \"dns01\"`. **Immutable**.",
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"acme_dns_credentials": schema.MapAttribute{
				MarkdownDescription: "DNS provider credentials for the `dns01` challenge (write-only — never " +
					"returned by the API). The expected keys depend on the provider (see " +
					"`ccp_acme_dns_providers`). Required when `acme_challenge = \"dns01\"`. **Immutable**.",
				Optional:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.RequiresReplace(),
				},
			},
			"acme_status": schema.StringAttribute{
				MarkdownDescription: "ACME issuance state: `pending` | `issued` | `failed`.",
				Computed:            true,
			},
			"acme_last_renewal_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last successful certificate renewal.",
				Computed:            true,
			},
			"acme_issued_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the current certificate was issued.",
				Computed:            true,
			},
			"acme_renew_after": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp after which the certificate is eligible for renewal.",
				Computed:            true,
			},
			"acme_last_error": schema.StringAttribute{
				MarkdownDescription: "Last ACME error message, if certificate issuance or renewal failed.",
				Computed:            true,
			},
			"tls_certificate_pem": schema.StringAttribute{
				MarkdownDescription: "With `acme_challenge = \"custom\"`: your certificate followed by its chain, " +
					"PEM-encoded. It must cover `hostname`; checked at submission (matching key, validity). " +
					"Changing it (or the key) **rotates the certificate in place**, without recreating the " +
					"listener. Never returned by the API.",
				Optional:  true,
				Sensitive: true,
			},
			"tls_private_key_pem": schema.StringAttribute{
				MarkdownDescription: "With `acme_challenge = \"custom\"`: the certificate's private key, " +
					"PEM-encoded and **not** protected by a passphrase. Write-only — never returned by the " +
					"API; the value you provide is kept in state.",
				Optional:  true,
				Sensitive: true,
			},
			"tls_cert_not_after": schema.StringAttribute{
				MarkdownDescription: "Expiry date of your certificate (`acme_challenge = \"custom\"`).",
				Computed:            true,
			},
			"tls_cert_names": schema.StringAttribute{
				MarkdownDescription: "Names covered by your certificate (`acme_challenge = \"custom\"`).",
				Computed:            true,
			},
			"tls_cert_issuer": schema.StringAttribute{
				MarkdownDescription: "Issuer of your certificate (`acme_challenge = \"custom\"`).",
				Computed:            true,
			},
			"cert_path": schema.StringAttribute{
				MarkdownDescription: "Server-side filesystem path of the live certificate. Informational.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
			},
		},
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

// ValidateConfig enforces the dns01 invariant at plan time: a clear error
// instead of a backend 400. Per provider pitfall #4, early-return when the
// challenge is unresolved (Null OR Unknown) so `terraform validate` does not
// fire before plan modifiers/defaults run.
func (r *listenerResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg listenerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateDNS01(cfg)...)
	resp.Diagnostics.Append(validateCustomCert(cfg)...)
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
	if v, ok := strVal(plan.AcmeChallenge); ok {
		createReq.AcmeChallenge = &v
	}
	if v, ok := strVal(plan.AcmeDNSProvider); ok {
		createReq.AcmeDNSProvider = &v
	}
	if !plan.AcmeDNSCredentials.IsNull() && !plan.AcmeDNSCredentials.IsUnknown() {
		creds := map[string]string{}
		resp.Diagnostics.Append(plan.AcmeDNSCredentials.ElementsAs(ctx, &creds, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if len(creds) > 0 {
			createReq.AcmeDNSCredentials = creds
		}
	}
	if v, ok := strVal(plan.TLSCertificatePEM); ok {
		createReq.TLSCertificatePEM = &v
	}
	if v, ok := strVal(plan.TLSPrivateKeyPEM); ok {
		createReq.TLSPrivateKeyPEM = &v
	}

	created, err := r.client.CreateAppGWListener(ctx, plan.AppGWID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create AppGW listener", err.Error())
		return
	}
	// applyToModel preserves the plan's write-only credentials (the API never
	// returns them).
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

// Update n'est atteint que par la rotation du certificat client : tout autre
// attribut porte RequiresReplace. Le nouveau certificat part sur la route de
// renouvellement, en place — recréer le listener couperait le trafic.
func (r *listenerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listenerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	appgwID, id := state.AppGWID.ValueString(), state.ID.ValueString()

	var got *client.AppGWListener
	var err error
	if certRotationNeeded(plan, state) {
		got, err = r.client.RotateAppGWListenerCert(ctx, appgwID, id, client.ListenerCertificateUpload{
			TLSCertificatePEM: plan.TLSCertificatePEM.ValueString(),
			TLSPrivateKeyPEM:  plan.TLSPrivateKeyPEM.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.AddError("Failed to rotate the listener certificate", err.Error())
			return
		}
	} else {
		// Rien à envoyer : on relit pour que les champs calculés soient connus.
		if got, err = r.client.GetAppGWListener(ctx, appgwID, id); err != nil {
			resp.Diagnostics.AddError("Failed to read AppGW listener", err.Error())
			return
		}
	}
	applyToModel(got, &plan)
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
// retrieved (the API requires both UUIDs). Write-only acme_dns_credentials
// cannot be recovered on import.
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
	return strings.SplitN(id, "/", 2)
}
