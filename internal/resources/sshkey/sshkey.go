// Package sshkey implements the ccp_ssh_key Terraform resource.
//
// SSH keys are simple, synchronous resources: POST returns the full object,
// DELETE returns 204, and there is no PATCH/PUT endpoint. `name`,
// `public_key` and `scope` therefore all force replacement on change.
//
// The CETIC Cloud API exposes no GET-by-id endpoint; the typed client emulates
// it via list-and-filter and surfaces a 404 APIError when the key is gone —
// callers detect that with client.IsNotFound for standard drift handling.
package sshkey

import (
	"context"
	"fmt"
	"regexp"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
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

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*sshKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*sshKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshKeyResource)(nil)
)

// New returns a freshly-constructed ccp_ssh_key resource. Wired in by
// provider.go via sshkey.New.
func New() resource.Resource {
	return &sshKeyResource{}
}

// sshKeyResource is the framework Resource implementation. The client is
// stashed in Configure and reused by Create/Read/Delete.
type sshKeyResource struct {
	client *client.Client
}

// sshKeyResourceModel mirrors the schema below 1-to-1. Tag names must match
// the schema attribute keys exactly.
type sshKeyResourceModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	PublicKey         types.String `tfsdk:"public_key"`
	Scope             types.String `tfsdk:"scope"`
	Fingerprint       types.String `tfsdk:"fingerprint"`
	CreatedByTenantID types.String `tfsdk:"created_by_tenant_id"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscore,
// hyphen, and space, max 100 chars (length enforced separately).
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\- ]+$`)

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an SSH public key registered with CETIC Cloud. " +
			"Keys are injected into containers (via cloud-init / authorized_keys) and VMs " +
			"(via cloud-init `sshkeys`). The CETIC Cloud API has no update endpoint, so any " +
			"change to `name`, `public_key` or `scope` forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the SSH key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (max 100 chars; alphanumerics, `_`, `-`, and spaces).",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(100),
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must contain only letters, digits, underscores, hyphens, or spaces",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "OpenSSH-format public key (e.g. `ssh-ed25519 AAAA... user@host`). " +
					"Send the entire single-line key as exported by `ssh-keygen`.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"scope": schema.StringAttribute{
				MarkdownDescription: "Visibility scope of the key. One of:\n" +
					"  * `user` — visible only to its creator, survives org switches (default — any member can create).\n" +
					"  * `org` — visible inside the currently active organization only (admin+/owner can create).\n" +
					"  * `tenant` — visible across every org and every invited member of the tenant (owner-only).\n\n" +
					"The CETIC Cloud API does not support mutating the scope of an existing key — any change " +
					"forces replacement (destroy + recreate).",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("user"),
				Validators: []validator.String{
					stringvalidator.OneOf("user", "org", "tenant"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"fingerprint": schema.StringAttribute{
				MarkdownDescription: "SHA-256 fingerprint computed by the API at creation time.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_by_tenant_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the tenant the key was created from. " +
					"Null on legacy keys predating the scoping migration.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the key was registered.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *sshKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		// Provider not yet configured (e.g. validate-only run). Nothing to do.
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider — please report it.", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// `scope` is Optional+Computed with a Default of "user", so it should be
	// Known by the time we land here — but be defensive: only send a non-empty
	// value, otherwise the backend applies its own default ("user").
	scope := ""
	if !plan.Scope.IsNull() && !plan.Scope.IsUnknown() {
		scope = plan.Scope.ValueString()
	}

	created, err := r.client.CreateSSHKey(ctx, client.SSHKeyCreateRequest{
		Name:      plan.Name.ValueString(),
		PublicKey: plan.PublicKey.ValueString(),
		Scope:     scope,
	})
	if err != nil {
		// 409 → duplicate fingerprint. Surface the API detail verbatim (it
		// arrives in French from FastAPI). Other errors fall through with the
		// same structured message.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"SSH key already exists",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create SSH key",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	plan.ID = types.StringValue(created.ID)
	plan.Name = types.StringValue(created.Name)
	plan.Fingerprint = types.StringValue(created.Fingerprint)
	plan.CreatedAt = types.StringValue(created.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	// Always reflect the server-side scope: if the user omitted it, the
	// backend stamps "user" — this materialises the Computed default into
	// state so the next plan is empty.
	if created.Scope != "" {
		plan.Scope = types.StringValue(created.Scope)
	} else {
		plan.Scope = types.StringValue("user")
	}
	if created.CreatedByTenantID != "" {
		plan.CreatedByTenantID = types.StringValue(created.CreatedByTenantID)
	} else {
		plan.CreatedByTenantID = types.StringNull()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetSSHKey(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: key was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read SSH key",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	state.Name = types.StringValue(got.Name)
	state.Fingerprint = types.StringValue(got.Fingerprint)
	state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	if got.Scope != "" {
		state.Scope = types.StringValue(got.Scope)
	} else {
		// Legacy rows predating the backend scoping migration return an empty
		// `scope` — collapse to the default ("user") to keep state coherent
		// with the Computed schema.
		state.Scope = types.StringValue("user")
	}
	if got.CreatedByTenantID != "" {
		state.CreatedByTenantID = types.StringValue(got.CreatedByTenantID)
	} else {
		state.CreatedByTenantID = types.StringNull()
	}
	// public_key is not returned by the API list; preserve whatever is in
	// state (set on Create or import-then-replace).

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: every mutable field has RequiresReplace, so the framework
// will never call this. Guard with a diagnostic in case someone changes the
// schema later without revisiting Update.
func (r *sshKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"ccp_ssh_key has no mutable attributes; all changes force replacement. "+
			"Reaching Update means the schema and the implementation are out of sync — please report this as a provider bug.",
	)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSSHKey(ctx, state.ID.ValueString()); err != nil {
		// Treat "already gone" as success — no point erroring on destroy when
		// the desired end state is already reached.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete SSH key",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}
}

// ImportState lets users adopt an existing key with `terraform import
// ccp_ssh_key.example <uuid>`. Read fills the rest. The `public_key`
// attribute remains unknown after import (the API never returns it); on the
// next plan Terraform will see a diff and propose a replace — this is
// expected and documented.
func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
