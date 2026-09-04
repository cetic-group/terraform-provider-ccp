// Package emailalias implements the ccp_email_alias resource — one source
// address rewritten to N destinations at delivery time.
//
// An alias stores nothing and costs nothing: it is also the group mechanism
// (`contact@` to a whole team), which is why `destinations` is a list rather
// than a single address.
package emailalias

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                   = (*emailAliasResource)(nil)
	_ resource.ResourceWithConfigure      = (*emailAliasResource)(nil)
	_ resource.ResourceWithImportState    = (*emailAliasResource)(nil)
	_ resource.ResourceWithValidateConfig = (*emailAliasResource)(nil)
)

func New() resource.Resource { return &emailAliasResource{} }

type emailAliasResource struct{ client *client.Client }

// Deliberately loose — one shape check, `local@domain`, and nothing more.
//
// Destinations very often point OUTSIDE the platform, at addresses whose local
// part is perfectly legal and looks exotic (`a+b`, `x!y`, `f/g`). A stricter
// pattern here would refuse an address the platform itself accepts, and the
// operator would have no way to tell which of the two was wrong. The platform
// is the authority on what a valid address is; the provider only catches the
// mistake worth catching at plan time — a value that is not an address at all.
//
// It also accepts a `*` local part, which is what a catch-all source looks
// like. Whether that is allowed HERE is not a regex question: see
// ValidateConfig.
var addressPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type emailAliasModel struct {
	ID           types.String `tfsdk:"id"`
	Address      types.String `tfsdk:"address"`
	Destinations types.List   `tfsdk:"destinations"`
	Wildcard     types.Bool   `tfsdk:"wildcard"`
	Comment      types.String `tfsdk:"comment"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (r *emailAliasResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_email_alias"
}

func (r *emailAliasResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a mail alias: one source address, rewritten to one or more " +
			"destinations at delivery. An alias stores nothing — it is not a mailbox — and several " +
			"destinations make it a distribution group.\n\n" +
			"`address` forces replacement: changing it is a different alias.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the alias.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "Source address, e.g. `contact@example.com`, or " +
					"`*@example.com` for a catch-all — which also requires `wildcard = true`. " +
					"Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(addressPattern,
						"must be a full address, e.g. contact@example.com, or *@example.com for a catch-all"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"destinations": schema.ListAttribute{
				MarkdownDescription: "Addresses mail is delivered to — inside or outside the " +
					"platform. 1 to 100 entries. Changed in place; the list replaces the previous " +
					"one whole.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeBetween(1, 100),
					listvalidator.ValueStringsAre(
						stringvalidator.RegexMatches(addressPattern, "must be a full email address"),
					),
				},
			},
			"wildcard": schema.BoolAttribute{
				MarkdownDescription: "Catch-all: takes every message addressed to an address of the " +
					"domain that does not exist. Off by default, and deliberately opt-in — it also " +
					"takes dictionary spam, and it hides for good the typos that should have " +
					"bounced.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			// Optional AND Computed for the same reason as on ccp_email_account:
			// the update route reads an omitted field as "leave it alone", so a
			// plain Optional would put null in state while the platform kept the
			// old text — and every plan would propose the same change forever.
			"comment": schema.StringAttribute{
				MarkdownDescription: "Free-form note about the alias (max 255 characters). " +
					"Removing the attribute keeps the current note — set it to `\"\"` to clear it.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.LengthAtMost(255)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// ValidateConfig catches `*@domain` declared with `wildcard = false` at plan
// time rather than at apply. Left to the API, the contradiction would surface
// as a 422 in the middle of an apply; caught here, it names the fix.
//
// Both values are checked for Unknown as well as Null: at `terraform validate`
// the `wildcard` default has not been applied yet, and treating Unknown as a
// concrete `false` would raise the error on a configuration that is fine
// (CLAUDE.md pitfall #4).
func (r *emailAliasResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg emailAliasModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Address.IsNull() || cfg.Address.IsUnknown() || cfg.Wildcard.IsNull() || cfg.Wildcard.IsUnknown() {
		return
	}
	if strings.HasPrefix(cfg.Address.ValueString(), "*@") && !cfg.Wildcard.ValueBool() {
		resp.Diagnostics.AddAttributeError(
			path.Root("wildcard"),
			"Catch-all address declared without the catch-all option",
			"`*@…` takes every message addressed to an address of the domain that does not exist. "+
				"Set `wildcard = true` to confirm that, or name a precise address.",
		)
	}
}

func (r *emailAliasResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func stateFrom(ctx context.Context, a *client.EmailAlias) (emailAliasModel, diag.Diagnostics) {
	dests, diags := types.ListValueFrom(ctx, types.StringType, a.Destinations)
	m := emailAliasModel{
		ID:           types.StringValue(a.ID),
		Address:      types.StringValue(a.Address),
		Destinations: dests,
		Wildcard:     types.BoolValue(a.Wildcard),
		CreatedAt:    types.StringValue(a.CreatedAt),
	}
	if a.Comment != nil {
		m.Comment = types.StringValue(*a.Comment)
	} else {
		m.Comment = types.StringNull()
	}
	return m, diags
}

func (r *emailAliasResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan emailAliasModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var dests []string
	resp.Diagnostics.Append(plan.Destinations.ElementsAs(ctx, &dests, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cr := client.EmailAliasCreateRequest{
		Address:      plan.Address.ValueString(),
		Destinations: dests,
		Wildcard:     plan.Wildcard.ValueBool(),
	}
	if !plan.Comment.IsNull() && !plan.Comment.IsUnknown() {
		v := plan.Comment.ValueString()
		cr.Comment = &v
	}

	created, err := r.client.CreateEmailAlias(ctx, cr)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create mail alias", err.Error())
		return
	}
	state, diags := stateFrom(ctx, created)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *emailAliasResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state emailAliasModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.GetEmailAlias(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read mail alias", err.Error())
		return
	}
	next, diags := stateFrom(ctx, a)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *emailAliasResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state emailAliasModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var dests []string
	resp.Diagnostics.Append(plan.Destinations.ElementsAs(ctx, &dests, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	wildcard := plan.Wildcard.ValueBool()

	patch := client.EmailAliasUpdateRequest{
		Destinations: &dests,
		Wildcard:     &wildcard,
	}
	if !plan.Comment.IsNull() && !plan.Comment.IsUnknown() {
		v := plan.Comment.ValueString()
		patch.Comment = &v
	}

	updated, err := r.client.UpdateEmailAlias(ctx, state.ID.ValueString(), patch)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update mail alias", err.Error())
		return
	}
	next, diags := stateFrom(ctx, updated)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *emailAliasResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state emailAliasModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteEmailAlias(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete mail alias", err.Error())
	}
}

func (r *emailAliasResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
