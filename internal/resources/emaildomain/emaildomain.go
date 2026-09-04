// Package emaildomain implements the ccp_email_domain resource.
//
// A hosted mail domain is born **on hold**: the name is reserved, nothing is
// routed, and the platform waits for a `TXT` record proving you own the
// domain. That shapes the whole resource:
//
//   - `verification` carries the record to publish, and `dns_records` the MX,
//     SPF, DKIM and DMARC lines expected in the public zone — each with the
//     state actually observed there. Both are computed: the apply does not
//     block waiting for a gesture that happens outside Terraform.
//   - `wait_for_verification` is the deliberate second apply that checks the
//     proof once the record is live. See its description.
//   - A `ccp_email_account` on a domain that is not `active` fails: the
//     mailbox would have nowhere to exist. Order the two with `depends_on`
//     when the DNS records are managed by another provider.
package emaildomain

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*emailDomainResource)(nil)
	_ resource.ResourceWithConfigure   = (*emailDomainResource)(nil)
	_ resource.ResourceWithImportState = (*emailDomainResource)(nil)
)

func New() resource.Resource { return &emailDomainResource{} }

type emailDomainResource struct{ client *client.Client }

// A record published at a registrar takes minutes to become visible, and the
// platform re-resolves it on each attempt.
//
// Variables rather than constants so a test can exercise the timeout path
// without waiting twenty minutes for it. Nothing else writes them.
var (
	verifyTimeout  = 20 * time.Minute
	verifyInterval = 30 * time.Second
)

const deleteWaitTimeout = 5 * time.Minute

// DNSRecordAttrTypes mirrors one expected DNS record. Exported so the data
// source builds the identical shape from the identical API payload.
var DNSRecordAttrTypes = map[string]attr.Type{
	"type":                 types.StringType,
	"name":                 types.StringType,
	"value":                types.StringType,
	"status":               types.StringType,
	"hostname":             types.StringType,
	"priority":             types.Int64Type,
	"exceeds_lookup_limit": types.BoolType,
	"purpose":              types.StringType,
}

// EndpointAttrTypes mirrors one mail-client endpoint.
var EndpointAttrTypes = map[string]attr.Type{
	"protocol": types.StringType,
	"hostname": types.StringType,
	"port":     types.Int64Type,
	"security": types.StringType,
}

// ClientConfigAttrTypes mirrors the mail-client settings block.
var ClientConfigAttrTypes = map[string]attr.Type{
	"incoming":      types.ObjectType{AttrTypes: EndpointAttrTypes},
	"outgoing":      types.ObjectType{AttrTypes: EndpointAttrTypes},
	"username_hint": types.StringType,
}

type emailDomainModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`

	WaitForVerification types.Bool `tfsdk:"wait_for_verification"`

	Status            types.String `tfsdk:"status"`
	VerifiedAt        types.String `tfsdk:"verified_at"`
	DKIMGeneratedAt   types.String `tfsdk:"dkim_generated_at"`
	ExternallyManaged types.Bool   `tfsdk:"externally_managed"`
	AccountsCount     types.Int64  `tfsdk:"accounts_count"`
	AliasesCount      types.Int64  `tfsdk:"aliases_count"`
	CreatedAt         types.String `tfsdk:"created_at"`

	Verification types.Object `tfsdk:"verification"`
	DNSRecords   types.List   `tfsdk:"dns_records"`
	ClientConfig types.Object `tfsdk:"client_config"`
}

func (r *emailDomainResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_email_domain"
}

// dnsRecordSchema is the shape of `verification` and of each `dns_records`
// entry — one description, used in both places.
func dnsRecordAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"type":  schema.StringAttribute{Computed: true, MarkdownDescription: "Record type: `MX`, `TXT`, `CNAME` or `TLSA`."},
		"name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the record to publish, e.g. `_dmarc.example.com`."},
		"value": schema.StringAttribute{Computed: true, MarkdownDescription: "Exact value to publish, complete — a `MX` includes its priority."},
		"status": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "State observed in the public zone, and the gesture it calls for:\n\n" +
				"- `ok` — nothing to do;\n" +
				"- `missing` — publish the line;\n" +
				"- `mismatch` — a different value is published; correct it;\n" +
				"- `conflict` — the zone carries several SPF records, which puts it in permanent " +
				"error; merge them into one line (`value` gives the merged one);\n" +
				"- `over_lookup_limit` — the published value is right, but the zone as a whole " +
				"exceeds the SPF lookup budget, so SPF fails for the entire domain.",
		},
		"hostname": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "For a `MX`, the server alone, without the trailing dot — DNS " +
				"control panels ask for the server and the priority in two separate fields. " +
				"Null for every other type, and null for a `MX` that could not be split; use " +
				"`value` then. Always comes as a pair with `priority`.",
		},
		"priority":             schema.Int64Attribute{Computed: true, MarkdownDescription: "For a `MX`, the priority. Paired with `hostname`."},
		"exceeds_lookup_limit": schema.BoolAttribute{Computed: true, MarkdownDescription: "The value shown is not publishable as-is: it would exceed the SPF lookup budget. Independent of `status`."},
		"purpose":              schema.StringAttribute{Computed: true, MarkdownDescription: "What the record is for, in one sentence."},
	}
}

func endpointAttributes(direction string) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"protocol": schema.StringAttribute{Computed: true, MarkdownDescription: fmt.Sprintf("Protocol of the %s server: `imap`, `pop3` or `smtp`.", direction)},
		"hostname": schema.StringAttribute{Computed: true, MarkdownDescription: "Server name to enter in the mail client."},
		"port":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Port to connect to."},
		"security": schema.StringAttribute{Computed: true, MarkdownDescription: "`tls` or `starttls`."},
	}
}

func (r *emailDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a hosted mail domain.\n\n" +
			"~> **The domain is created on hold.** It is not routed until you publish the record " +
			"given in `verification` in the public DNS of the domain, and the platform has seen it. " +
			"Creating a `ccp_email_account` before that fails — the mailbox would have nowhere to " +
			"exist.\n\n" +
			"The usual sequence is two applies: the first declares the domain and hands you the " +
			"records to publish; the second, with `wait_for_verification = true`, activates it. " +
			"When the DNS records are managed by another provider in the same configuration, order " +
			"them with `depends_on`.\n\n" +
			"`name` forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the domain.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Domain to host, e.g. `example.com`. A mail domain has a single " +
					"owner, so the name is claimed platform-wide as soon as it is declared. Forces " +
					"replacement.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"wait_for_verification": schema.BoolAttribute{
				MarkdownDescription: "Leave at `false` (the default) on the apply that creates the " +
					"domain: it returns immediately with the records to publish.\n\n" +
					"Once the record named in `verification` is live in the public DNS of the " +
					"domain, set this to `true` and apply again — that apply asks the platform to " +
					"check the proof and waits until the domain is `active`.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending_verification` (name reserved, nothing routed), " +
					"`active`, or `suspended` (routing cut by the platform; configuration and " +
					"mailboxes are kept).",
				Computed: true,
			},
			"verified_at": schema.StringAttribute{
				MarkdownDescription: "When ownership was established. Null while the domain waits.",
				Computed:            true,
			},
			"dkim_generated_at": schema.StringAttribute{
				MarkdownDescription: "When the signing key was created. Null means no key exists " +
					"yet — distinct from a key that exists but is not published in your zone, which " +
					"reads from `dns_records`.",
				Computed: true,
			},
			"externally_managed": schema.BoolAttribute{
				MarkdownDescription: "Whether the CCP console is read-only on this domain because " +
					"it is driven from infrastructure code. Set by the platform, not by this " +
					"resource: one domain has one control plane, and two of them always diverge.",
				Computed: true,
			},
			"accounts_count": schema.Int64Attribute{
				MarkdownDescription: "Number of mailboxes on the domain.",
				Computed:            true,
			},
			"aliases_count": schema.Int64Attribute{
				MarkdownDescription: "Number of aliases on the domain.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"verification": schema.SingleNestedAttribute{
				MarkdownDescription: "The `TXT` record proving you own the domain — the only one " +
					"that blocks activation. Kept apart from `dns_records` for that reason.",
				Computed:   true,
				Attributes: dnsRecordAttributes(),
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "The `MX`, SPF, DKIM and DMARC records expected in the public " +
					"zone of the domain, each with the state observed there. The list is what the " +
					"platform can determine at read time: when the mail server cannot be reached, " +
					"only the records that depend on us alone (SPF, DMARC) are returned.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: dnsRecordAttributes(),
				},
			},
			"client_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Settings to copy into a mail application.",
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"incoming": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Incoming mail server.",
						Attributes:          endpointAttributes("incoming"),
					},
					"outgoing": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Outgoing mail server.",
						Attributes:          endpointAttributes("outgoing"),
					},
					"username_hint": schema.StringAttribute{
						Computed: true,
						MarkdownDescription: "Reminder that the user name is the full address — " +
							"entering only the local part is the first cause of failed setups.",
					},
				},
			},
		},
	}
}

func (r *emailDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// DNSRecordObject converts one expected DNS record into its Terraform shape.
func DNSRecordObject(rec client.EmailDomainDNSRecord) (types.Object, diag.Diagnostics) {
	hostname := types.StringNull()
	if rec.Hostname != nil {
		hostname = types.StringValue(*rec.Hostname)
	}
	priority := types.Int64Null()
	if rec.Priority != nil {
		priority = types.Int64Value(*rec.Priority)
	}
	return types.ObjectValue(DNSRecordAttrTypes, map[string]attr.Value{
		"type":                 types.StringValue(rec.Type),
		"name":                 types.StringValue(rec.Name),
		"value":                types.StringValue(rec.Value),
		"status":               types.StringValue(rec.Status),
		"hostname":             hostname,
		"priority":             priority,
		"exceeds_lookup_limit": types.BoolValue(rec.ExceedsLookupLimit),
		"purpose":              types.StringValue(rec.Purpose),
	})
}

// ClientConfigObject converts the mail-client settings block.
func ClientConfigObject(cfg *client.EmailClientConfig) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if cfg == nil {
		return types.ObjectNull(ClientConfigAttrTypes), diags
	}
	endpoint := func(e client.EmailClientEndpoint) types.Object {
		obj, d := types.ObjectValue(EndpointAttrTypes, map[string]attr.Value{
			"protocol": types.StringValue(e.Protocol),
			"hostname": types.StringValue(e.Hostname),
			"port":     types.Int64Value(e.Port),
			"security": types.StringValue(e.Security),
		})
		diags.Append(d...)
		return obj
	}
	obj, d := types.ObjectValue(ClientConfigAttrTypes, map[string]attr.Value{
		"incoming":      endpoint(cfg.Incoming),
		"outgoing":      endpoint(cfg.Outgoing),
		"username_hint": types.StringValue(cfg.UsernameHint),
	})
	diags.Append(d...)
	return obj, diags
}

// stateFrom maps the API domain onto the model. `wantWait` is carried from the
// plan: `wait_for_verification` is a provider-side switch the API knows
// nothing about, and mapping it from a response would blank it.
func stateFrom(d *client.EmailDomain, wantWait types.Bool) (emailDomainModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := emailDomainModel{
		ID:                  types.StringValue(d.ID),
		Name:                types.StringValue(d.Name),
		WaitForVerification: wantWait,
		Status:              types.StringValue(d.Status),
		ExternallyManaged:   types.BoolValue(d.ExternallyManaged),
		CreatedAt:           types.StringValue(d.CreatedAt),
	}
	if wantWait.IsNull() || wantWait.IsUnknown() {
		m.WaitForVerification = types.BoolValue(false)
	}
	if d.VerifiedAt != nil {
		m.VerifiedAt = types.StringValue(*d.VerifiedAt)
	} else {
		m.VerifiedAt = types.StringNull()
	}
	if d.DKIMGeneratedAt != nil {
		m.DKIMGeneratedAt = types.StringValue(*d.DKIMGeneratedAt)
	} else {
		m.DKIMGeneratedAt = types.StringNull()
	}
	if d.AccountsCount != nil {
		m.AccountsCount = types.Int64Value(*d.AccountsCount)
	} else {
		m.AccountsCount = types.Int64Null()
	}
	if d.AliasesCount != nil {
		m.AliasesCount = types.Int64Value(*d.AliasesCount)
	} else {
		m.AliasesCount = types.Int64Null()
	}

	// The list shape of the API carries none of the three blocks below. Writing
	// zero values over them would be CLAUDE.md pitfall #5 — so absent stays
	// null, and only the single-domain GET fills them in.
	if d.Verification == nil {
		m.Verification = types.ObjectNull(DNSRecordAttrTypes)
	} else {
		obj, dd := DNSRecordObject(*d.Verification)
		diags.Append(dd...)
		m.Verification = obj
	}

	recordType := types.ObjectType{AttrTypes: DNSRecordAttrTypes}
	if d.Records == nil {
		m.DNSRecords = types.ListNull(recordType)
	} else {
		elems := make([]attr.Value, 0, len(d.Records))
		for _, rec := range d.Records {
			obj, dd := DNSRecordObject(rec)
			diags.Append(dd...)
			elems = append(elems, obj)
		}
		list, dd := types.ListValue(recordType, elems)
		diags.Append(dd...)
		m.DNSRecords = list
	}

	cfg, dd := ClientConfigObject(d.ClientConfig)
	diags.Append(dd...)
	m.ClientConfig = cfg

	return m, diags
}

func (r *emailDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan emailDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateEmailDomain(ctx, client.EmailDomainCreateRequest{
		Name: plan.Name.ValueString(),
	})
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Mail domain already claimed",
				err.Error()+"\n\nA mail domain has a single owner, so its name is claimed "+
					"platform-wide. If your organisation already declared it, import it instead: "+
					"`terraform import ccp_email_domain.<name> <domain_id>`.",
			)
			return
		}
		resp.Diagnostics.AddError("Failed to create mail domain", err.Error())
		return
	}

	// The POST answers with the list shape. Read the domain back so the state
	// carries the records to publish — which is the whole point of the apply
	// when the domain is still on hold.
	//
	// ⚠️ State is written even when the domain does not activate. From here on
	// the name is claimed PLATFORM-WIDE: returning on the error without
	// recording the domain would leave it claimed and unreferenced, and the
	// next apply could neither create it (409) nor destroy it.
	final := r.settle(ctx, created.ID, plan.Name.ValueString(), plan.WaitForVerification.ValueBool(), &resp.Diagnostics)
	if final == nil {
		final = created
	}

	state, diags := stateFrom(final, plan.WaitForVerification)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// settle brings the domain as far as it can go, and says out loud what is left
// to do otherwise. Waiting for a proof of ownership is never an apply failure:
// the domain exists and the record to publish is in state, and failing would
// throw that information away.
func (r *emailDomainResource) settle(ctx context.Context, id, name string, wait bool, diags *diag.Diagnostics) *client.EmailDomain {
	if wait {
		final, err := r.pollVerify(ctx, id)
		if err != nil {
			diags.AddError("Domain ownership not established", err.Error())
			// Fall through to a plain read: the caller still has to record the
			// domain, which exists whether or not the proof went through.
			if fallback, readErr := r.client.GetEmailDomain(ctx, id); readErr == nil {
				return fallback
			}
			return nil
		}
		return final
	}

	final, err := r.client.GetEmailDomain(ctx, id)
	if err != nil {
		diags.AddError("Failed to read mail domain", err.Error())
		return nil
	}
	if final.Status == client.EmailDomainStatusPendingVerification {
		diags.AddWarning(
			"Mail domain waiting for proof of ownership",
			fmt.Sprintf(
				"The domain %q is not routing mail yet. Publish the record given in "+
					"`verification` in its public DNS, then set `wait_for_verification = true` "+
					"and apply again. A `ccp_email_account` on this domain will fail until then.",
				name,
			),
		)
	}
	return final
}

// pollVerify replays POST /verify until the platform accepts the proof. The
// call is idempotent and a rejection is not an error state: the record is
// simply not visible yet.
func (r *emailDomainResource) pollVerify(ctx context.Context, id string) (*client.EmailDomain, error) {
	deadline := time.Now().Add(verifyTimeout)
	var lastErr error
	for {
		d, err := r.client.VerifyEmailDomain(ctx, id)
		if err == nil && d.Status != client.EmailDomainStatusPendingVerification {
			// The verify answer is the list shape; read back for the records
			// and the mail-client settings.
			return r.client.GetEmailDomain(ctx, id)
		}
		if err != nil {
			if client.IsNotFound(err) {
				return nil, err
			}
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return nil, fmt.Errorf(
					"the ownership record was still not visible after %s — last answer: %w",
					verifyTimeout, lastErr,
				)
			}
			return nil, fmt.Errorf("the ownership record was still not visible after %s", verifyTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(verifyInterval):
		}
	}
}

func (r *emailDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state emailDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.client.GetEmailDomain(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read mail domain", err.Error())
		return
	}
	next, diags := stateFrom(d, state.WaitForVerification)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

// Update handles the only non-replacing attribute: `wait_for_verification`.
// Flipping it to `true` is the deliberate second apply that checks the proof.
func (r *emailDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state emailDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	final := r.settle(ctx, state.ID.ValueString(), state.Name.ValueString(),
		plan.WaitForVerification.ValueBool(), &resp.Diagnostics)
	if final == nil {
		return
	}

	next, diags := stateFrom(final, plan.WaitForVerification)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *emailDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state emailDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if err := r.client.DeleteEmailDomain(ctx, id); err != nil {
		if client.IsNotFound(err) {
			return
		}
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Mail domain still carries mailboxes or aliases",
				err.Error()+"\n\nDeleting a domain is not a cascade: it would destroy mailboxes "+
					"and their content. Remove the `ccp_email_account` and `ccp_email_alias` "+
					"resources first — Terraform does this on its own when they are declared in "+
					"the same configuration.",
			)
			return
		}
		resp.Diagnostics.AddError("Failed to delete mail domain", err.Error())
		return
	}
	// The name is claimed platform-wide, so a replace that re-enters Create too
	// early is answered with a 409.
	if err := client.PollUntilDeleted(ctx, deleteWaitTimeout, func(ctx context.Context) error {
		_, err := r.client.GetEmailDomain(ctx, id)
		return err
	}); err != nil {
		resp.Diagnostics.AddError("Mail domain deletion did not complete", err.Error())
	}
}

func (r *emailDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("wait_for_verification"), false)...)
}
