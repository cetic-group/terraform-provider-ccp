// Package emailaccount implements the ccp_email_account resource — a mailbox
// on a hosted mail domain.
//
// Two things about this resource do not follow from its schema:
//
//   - `password` is **write only**. No response of the mail API carries a
//     password, in any shape. Terraform keeps the configured value in state
//     and that is the only copy it has: changing it in the configuration
//     resets the mailbox password, and nothing can ever detect a password
//     changed elsewhere.
//   - The quota is written in gigabytes and read back in bytes. They are two
//     different attributes here on purpose: `quota_gb` is the intent, and
//     `quota_bytes` is what the platform reserved — and bills.
package emailaccount

import (
	"context"
	"fmt"
	"regexp"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/cetic-group/terraform-provider-ccp/internal/resources/emaildomain"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*emailAccountResource)(nil)
	_ resource.ResourceWithConfigure   = (*emailAccountResource)(nil)
	_ resource.ResourceWithImportState = (*emailAccountResource)(nil)
)

func New() resource.Resource { return &emailAccountResource{} }

type emailAccountResource struct{ client *client.Client }

// A mailbox password is proved directly against IMAP and SMTP, which are open
// to the internet with no captcha in front: the floor is higher than the
// console's.
const (
	passwordMinLength = 12
	passwordMaxLength = 128
	// The platform stores `quota_gb * gibibyte` exactly, and bills on the
	// result. The two units are not interchangeable, which is why they are two
	// attributes here — and why converting between them needs a stated rule.
	gibibyte = 1024 * 1024 * 1024
)

// Deliberately loose — one shape check, `local@domain`, and nothing more.
//
// Forwarding destinations very often point OUTSIDE the platform, at addresses
// whose local part is perfectly legal and looks exotic (`a+b`, `x!y`, `f/g`). A
// stricter pattern would refuse an address the platform itself accepts, and the
// operator would have no way to tell which of the two was wrong. The platform
// is the authority on what a valid address is; this only catches the mistake
// worth catching at plan time — a value that is not an address at all.
var addressPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type emailAccountModel struct {
	ID       types.String `tfsdk:"id"`
	Address  types.String `tfsdk:"address"`
	Password types.String `tfsdk:"password"`

	QuotaGB       types.Int64  `tfsdk:"quota_gb"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	EnableIMAP    types.Bool   `tfsdk:"enable_imap"`
	EnablePOP     types.Bool   `tfsdk:"enable_pop"`
	Comment       types.String `tfsdk:"comment"`
	DisplayedName types.String `tfsdk:"displayed_name"`

	ForwardEnabled     types.Bool `tfsdk:"forward_enabled"`
	ForwardDestination types.List `tfsdk:"forward_destination"`
	ForwardKeep        types.Bool `tfsdk:"forward_keep"`

	QuotaBytes       types.Int64  `tfsdk:"quota_bytes"`
	UsageBytes       types.Int64  `tfsdk:"usage_bytes"`
	UsageUpdatedAt   types.String `tfsdk:"usage_updated_at"`
	IsSystemManaged  types.Bool   `tfsdk:"is_system_managed"`
	SendAsAnyAddress types.Bool   `tfsdk:"send_as_any_address"`
	SendAsPending    types.Bool   `tfsdk:"send_as_pending"`
	CreatedAt        types.String `tfsdk:"created_at"`
	ClientConfig     types.Object `tfsdk:"client_config"`
}

func (r *emailAccountResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_email_account"
}

func (r *emailAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a mailbox on a hosted mail domain.\n\n" +
			"~> **The domain must already be active.** A mailbox on a domain still waiting for its " +
			"proof of ownership is refused — it would have nowhere to exist. Order the two with " +
			"`depends_on` when the DNS records come from another provider.\n\n" +
			"~> **`password` is write only.** The platform never returns it, so the value in your " +
			"configuration is the only copy Terraform has. Changing it resets the mailbox password; " +
			"a password changed outside Terraform cannot be detected.\n\n" +
			"`address` forces replacement: renaming a mailbox would move mail already delivered and " +
			"break every forwarding rule pointing at it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the mailbox.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "Full address to create, e.g. `contact@example.com`. Its domain " +
					"part designates the `ccp_email_domain`. Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(addressPattern, "must be a full email address, e.g. contact@example.com"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password": schema.StringAttribute{
				MarkdownDescription: fmt.Sprintf(
					"Mailbox password for IMAP, POP3 and SMTP (%d to %d characters). Write only: it "+
						"is never returned. Change it here to reset it.",
					passwordMinLength, passwordMaxLength),
				Required:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(passwordMinLength, passwordMaxLength),
				},
			},
			// Optional AND Computed, for the same reason as `comment` below: the
			// update route reads an omitted quota as "leave it alone" — it cannot
			// un-set one.
			//
			// Left plain Optional, removing the attribute after having set it
			// planned `20 → null`, the API ignored the absent field, and the
			// mailbox stayed reserved — and BILLED — at 20 GiB while the state
			// recorded null. The next plan then converged on that null, so nothing
			// ever surfaced the gap again: a silent divergence, which is worse than
			// a diff that keeps coming back.
			//
			// Computed makes the removal mean "keep what is reserved", which is
			// what the platform actually does. To go back to the platform default,
			// write that value explicitly — a quota is changed, never un-set.
			"quota_gb": schema.Int64Attribute{
				MarkdownDescription: "Space reserved for the mailbox, in gigabytes (1 to 1024). " +
					"Omit on creation to take the platform default. Changed in place, but " +
					"**never below what the mailbox already holds** — the platform refuses that.\n\n" +
					"~> A quota is changed, never un-set: removing the attribute keeps the space " +
					"already reserved (and billed). Write the value you want instead.\n\n" +
					"`quota_bytes` reports what was actually reserved, to the byte.",
				Optional: true,
				Computed: true,
				Validators: []validator.Int64{
					int64validator.Between(1, 1024),
				},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the mailbox accepts logins and deliveries. Turning it " +
					"off deletes nothing and frees nothing: the space stays reserved, so it stays " +
					"billed. Defaults to `true`.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"enable_imap": schema.BoolAttribute{
				MarkdownDescription: "Whether IMAP is available on this mailbox. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"enable_pop": schema.BoolAttribute{
				MarkdownDescription: "Whether POP3 is available. Defaults to `false`: POP3 downloads " +
					"and deletes, which surprises anyone reading mail on more than one device.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			// Optional AND Computed, although the platform sets no default here.
			//
			// The update route reads an omitted field as "leave it alone" — it has
			// no way to say "clear it". Left plain Optional, removing the attribute
			// from the configuration would put null in state while the platform kept
			// the old text, and every subsequent plan would propose the same change
			// again, forever. Computed makes the removal mean "keep what is there",
			// which is what the platform actually does; write an empty string to
			// clear the field.
			"comment": schema.StringAttribute{
				MarkdownDescription: "Free-form note about the mailbox (max 255 characters). " +
					"Removing the attribute keeps the current note — set it to `\"\"` to clear it.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.LengthAtMost(255)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"displayed_name": schema.StringAttribute{
				MarkdownDescription: "Name shown as the sender, e.g. `Sales` (max 160 characters). " +
					"Removing the attribute keeps the current name — set it to `\"\"` to clear it.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.LengthAtMost(160)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"forward_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether incoming mail is forwarded to `forward_destination`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"forward_destination": schema.ListAttribute{
				MarkdownDescription: "Addresses mail is forwarded to (up to 20).",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtMost(20),
					listvalidator.ValueStringsAre(
						stringvalidator.RegexMatches(addressPattern, "must be a full email address"),
					),
				},
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"forward_keep": schema.BoolAttribute{
				MarkdownDescription: "Whether forwarded mail is also kept in the mailbox. Setting " +
					"this to `false` has consequences: without a local copy, a wrong destination " +
					"loses the mail for good.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"quota_bytes": schema.Int64Attribute{
				MarkdownDescription: "Space actually reserved, in bytes — the billing basis. Never " +
					"the space used.",
				Computed: true,
			},
			"usage_bytes": schema.Int64Attribute{
				MarkdownDescription: "Last reading of the space occupied. Null while no reading has " +
					"succeeded, which is not the same as an empty mailbox. Always read it together " +
					"with `usage_updated_at`: it can be hours old.",
				Computed: true,
			},
			"usage_updated_at": schema.StringAttribute{
				MarkdownDescription: "When `usage_bytes` was last read.",
				Computed:            true,
			},
			"is_system_managed": schema.BoolAttribute{
				MarkdownDescription: "Whether the platform owns this mailbox — the address that " +
					"collects the domain's DMARC reports is one. Always `false` for mailboxes " +
					"created here.",
				Computed: true,
			},
			"send_as_any_address": schema.BoolAttribute{
				MarkdownDescription: "Whether the mailbox may send under any address of its domain. " +
					"Read only here: it is a privilege of its own, granted through its own API call " +
					"and its own permission, so that managing mailboxes can be delegated without it.",
				Computed: true,
			},
			"send_as_pending": schema.BoolAttribute{
				MarkdownDescription: "`true` when `send_as_any_address` is recorded but not yet " +
					"applied by the mail server — sending under another address is still refused.",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"client_config": schema.SingleNestedAttribute{
				MarkdownDescription: "Settings to copy into a mail application, computed **for this " +
					"mailbox**: the incoming server follows its POP3 flag.",
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"incoming": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Incoming mail server.",
						Attributes:          endpointAttributes(),
					},
					"outgoing": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Outgoing mail server.",
						Attributes:          endpointAttributes(),
					},
					"username_hint": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Reminder that the user name is the full address.",
					},
				},
			},
		},
	}
}

func endpointAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"protocol": schema.StringAttribute{Computed: true, MarkdownDescription: "`imap`, `pop3` or `smtp`."},
		"hostname": schema.StringAttribute{Computed: true, MarkdownDescription: "Server name to enter in the mail client."},
		"port":     schema.Int64Attribute{Computed: true, MarkdownDescription: "Port to connect to."},
		"security": schema.StringAttribute{Computed: true, MarkdownDescription: "`tls` or `starttls`."},
	}
}

func (r *emailAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// quotaGBFrom expresses the space the platform actually reserved, in whole
// gigabytes.
//
// The configured value wins when it matches to the byte: it is what the
// operator wrote, and rewriting it would be churn — worse, on a Required-like
// value it is what raises "inconsistent result after apply".
//
// Otherwise the RESERVED space is reported, rounded UP. Rounding up is the
// platform's own convention when it has to express a byte quota in gigabytes,
// and it is the safe direction: understating a quota that is billed in full
// would invite the operator to write back a smaller number, which the platform
// would then apply.
func quotaGBFrom(quotaBytes int64, configured types.Int64) types.Int64 {
	if !configured.IsNull() && !configured.IsUnknown() && configured.ValueInt64()*gibibyte == quotaBytes {
		return configured
	}
	if quotaBytes <= 0 {
		return types.Int64Null()
	}
	return types.Int64Value((quotaBytes + gibibyte - 1) / gibibyte)
}

// stateFrom maps the API mailbox onto the model.
//
// `password` is carried over from the configuration, not from the response: no
// response of this API carries one, and writing it from the response would
// blank the only copy Terraform holds — the shape of CLAUDE.md pitfall #5, with
// a secret at stake.
//
// `quota_gb` is NOT carried over blindly. The API answers in bytes, and the
// space it reserved is the truth: taking the configured value on faith is what
// let a removed attribute record `null` over a mailbox still reserved at 20 GiB.
func stateFrom(ctx context.Context, a *client.EmailAccount, password types.String, quotaGB types.Int64) (emailAccountModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	forwards, d := types.ListValueFrom(ctx, types.StringType, a.ForwardDestination)
	diags.Append(d...)

	cfg, d := emaildomain.ClientConfigObject(a.ClientConfig)
	diags.Append(d...)

	m := emailAccountModel{
		ID:                 types.StringValue(a.ID),
		Address:            types.StringValue(a.Address),
		Password:           password,
		QuotaGB:            quotaGBFrom(a.QuotaBytes, quotaGB),
		Enabled:            types.BoolValue(a.Enabled),
		EnableIMAP:         types.BoolValue(a.EnableIMAP),
		EnablePOP:          types.BoolValue(a.EnablePOP),
		ForwardEnabled:     types.BoolValue(a.ForwardEnabled),
		ForwardDestination: forwards,
		ForwardKeep:        types.BoolValue(a.ForwardKeep),
		QuotaBytes:         types.Int64Value(a.QuotaBytes),
		IsSystemManaged:    types.BoolValue(a.IsSystemManaged),
		SendAsAnyAddress:   types.BoolValue(a.SendAsAnyAddress),
		SendAsPending:      types.BoolValue(a.SendAsPending),
		CreatedAt:          types.StringValue(a.CreatedAt),
		ClientConfig:       cfg,
	}
	if a.UsageBytes != nil {
		m.UsageBytes = types.Int64Value(*a.UsageBytes)
	} else {
		m.UsageBytes = types.Int64Null()
	}
	if a.UsageUpdatedAt != nil {
		m.UsageUpdatedAt = types.StringValue(*a.UsageUpdatedAt)
	} else {
		m.UsageUpdatedAt = types.StringNull()
	}
	if a.Comment != nil {
		m.Comment = types.StringValue(*a.Comment)
	} else {
		m.Comment = types.StringNull()
	}
	if a.DisplayedName != nil {
		m.DisplayedName = types.StringValue(*a.DisplayedName)
	} else {
		m.DisplayedName = types.StringNull()
	}
	return m, diags
}

func optString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func optBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// quotaChange returns the quota to send, or nil when the plan asks for exactly
// what is already reserved.
func quotaChange(planned types.Int64, reservedBytes types.Int64) *int64 {
	if planned.IsNull() || planned.IsUnknown() {
		return nil
	}
	want := planned.ValueInt64()
	if !reservedBytes.IsNull() && !reservedBytes.IsUnknown() && want*gibibyte == reservedBytes.ValueInt64() {
		return nil
	}
	return &want
}

func optInt64(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func (r *emailAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan emailAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateEmailAccount(ctx, client.EmailAccountCreateRequest{
		Address:       plan.Address.ValueString(),
		Password:      plan.Password.ValueString(),
		QuotaGB:       optInt64(plan.QuotaGB),
		Comment:       optString(plan.Comment),
		DisplayedName: optString(plan.DisplayedName),
		EnableIMAP:    optBool(plan.EnableIMAP),
		EnablePOP:     optBool(plan.EnablePOP),
	})
	if err != nil {
		addWriteError(&resp.Diagnostics, "Failed to create mailbox", err)
		return
	}

	// `enabled` and the forwarding settings have no place in the creation
	// payload — the API only accepts them on update. Left out, a configuration
	// asking for `enabled = false` would silently come back enabled, and
	// Terraform would reject the apply for an inconsistent result.
	if patch, needed := forwardingPatch(ctx, plan, &resp.Diagnostics); needed {
		if resp.Diagnostics.HasError() {
			return
		}
		if _, err := r.client.UpdateEmailAccount(ctx, created.ID, patch); err != nil {
			addWriteError(&resp.Diagnostics, "Mailbox created, but its settings could not be applied", err)
			return
		}
	}

	// Read back for `client_config`, which the creation response does not carry.
	final, err := r.client.GetEmailAccount(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the mailbox back", err.Error())
		return
	}

	state, diags := stateFrom(ctx, final, plan.Password, plan.QuotaGB)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// forwardingPatch builds the follow-up update for the settings the creation
// route does not accept. Returns false when there is nothing to send.
func forwardingPatch(ctx context.Context, plan emailAccountModel, diags *diag.Diagnostics) (client.EmailAccountUpdateRequest, bool) {
	var patch client.EmailAccountUpdateRequest
	needed := false

	// Only when it departs from the server's own default: an update on every
	// create would double the writes on the shared mail server for nothing.
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() && !plan.Enabled.ValueBool() {
		patch.Enabled = optBool(plan.Enabled)
		needed = true
	}
	if b := optBool(plan.ForwardEnabled); b != nil {
		patch.ForwardEnabled = b
		needed = true
	}
	if b := optBool(plan.ForwardKeep); b != nil {
		patch.ForwardKeep = b
		needed = true
	}
	if !plan.ForwardDestination.IsNull() && !plan.ForwardDestination.IsUnknown() {
		var dests []string
		diags.Append(plan.ForwardDestination.ElementsAs(ctx, &dests, false)...)
		patch.ForwardDestination = &dests
		needed = true
	}
	return patch, needed
}

func (r *emailAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state emailAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.GetEmailAccount(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read mailbox", err.Error())
		return
	}
	next, diags := stateFrom(ctx, a, state.Password, state.QuotaGB)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *emailAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state emailAccountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	// The password has its own route: it is not stored on our side, so there
	// is nothing to update — only to reset.
	if !plan.Password.Equal(state.Password) && !plan.Password.IsNull() && !plan.Password.IsUnknown() {
		if err := r.client.ResetEmailAccountPassword(ctx, id, plan.Password.ValueString()); err != nil {
			addWriteError(&resp.Diagnostics, "Failed to reset the mailbox password", err)
			return
		}
	}

	patch := client.EmailAccountUpdateRequest{
		// Sent only when the operator actually wants a DIFFERENT quota.
		//
		// `quota_gb` is now always concrete in the plan (Optional + Computed), so
		// forwarding it unconditionally would resend, on every unrelated edit, the
		// value derived from the reserved bytes. On a quota that is not a whole
		// number of gigabytes that derived value is rounded UP — resending it would
		// quietly GROW the mailbox, and the bill, because someone renamed a sender.
		QuotaGB:        quotaChange(plan.QuotaGB, state.QuotaBytes),
		Enabled:        optBool(plan.Enabled),
		Comment:        optString(plan.Comment),
		DisplayedName:  optString(plan.DisplayedName),
		EnableIMAP:     optBool(plan.EnableIMAP),
		EnablePOP:      optBool(plan.EnablePOP),
		ForwardEnabled: optBool(plan.ForwardEnabled),
		ForwardKeep:    optBool(plan.ForwardKeep),
	}
	if !plan.ForwardDestination.IsNull() && !plan.ForwardDestination.IsUnknown() {
		var dests []string
		resp.Diagnostics.Append(plan.ForwardDestination.ElementsAs(ctx, &dests, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		patch.ForwardDestination = &dests
	}

	if _, err := r.client.UpdateEmailAccount(ctx, id, patch); err != nil {
		addWriteError(&resp.Diagnostics, "Failed to update mailbox", err)
		return
	}

	final, err := r.client.GetEmailAccount(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the mailbox back", err.Error())
		return
	}
	next, diags := stateFrom(ctx, final, plan.Password, plan.QuotaGB)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *emailAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state emailAccountModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteEmailAccount(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		addWriteError(&resp.Diagnostics, "Failed to delete mailbox", err)
	}
}

// addWriteError relays the API message as-is.
//
// Its refusals are already written for the customer: the 409 names the domain
// that is not active yet, and the 422 the space the mailbox already holds.
// Rewording them here would remove the only actionable part.
func addWriteError(diags *diag.Diagnostics, summary string, err error) {
	diags.AddError(summary, err.Error())
}

// ImportState takes the mailbox UUID.
//
// `password` cannot be imported — the platform does not hold it. The first
// plan after an import therefore shows a change on that attribute, and
// applying it resets the mailbox password to the configured value. That is
// deliberate: the alternative would be a state claiming to know a secret it
// does not.
func (r *emailAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
