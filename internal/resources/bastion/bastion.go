// Package bastion implements the ccp_bastion Terraform resource.
//
// A Bastion is a managed secure-SSH-access appliance that fronts the private
// instances of a VPC: operators reach their otherwise-unreachable private
// hosts through a single, audited entry point instead of exposing every
// instance to the public internet.
//
// CRUD semantics:
//   - Create : POST /v1/bastions — returns the appliance metadata. The SSH
//     endpoint (`endpoint_host` / `endpoint_port`) is populated once the
//     appliance finishes provisioning, so both are Computed.
//   - Read   : GET /v1/bastions/{id}. 404 ⇒ removed from state (drift).
//   - Delete : DELETE /v1/bastions/{id}, then poll until the appliance is
//     really gone — teardown is asynchronous, and without the wait a replace
//     (destroy-then-create with the same name) would race the still-present
//     appliance and get a 409.
//
// Every mutable field (`name`, `region`, `vpc_id`) forces replacement: the
// CETIC Cloud API exposes no update endpoint for bastions. Update is therefore
// a guarded no-op (the framework never calls it).
package bastion

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*bastionResource)(nil)
	_ resource.ResourceWithConfigure   = (*bastionResource)(nil)
	_ resource.ResourceWithImportState = (*bastionResource)(nil)
)

// New returns a freshly-constructed ccp_bastion resource. Wired in by
// provider.go via bastion.New.
func New() resource.Resource {
	return &bastionResource{}
}

// bastionResource is the framework Resource implementation. The client is
// stashed in Configure and reused by Create/Read/Delete.
type bastionResource struct {
	client *client.Client
}

// bastionResourceModel mirrors the schema below 1-to-1. Tag names must match
// the schema attribute keys exactly.
type bastionResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	Plan            types.String `tfsdk:"plan"`
	VpcID           types.String `tfsdk:"vpc_id"`
	VpcIDs          types.List   `tfsdk:"vpc_ids"`
	PublicIPID      types.String `tfsdk:"public_ip_id"`
	Tags            types.List   `tfsdk:"tags"`
	Status          types.String `tfsdk:"status"`
	EndpointHost    types.String `tfsdk:"endpoint_host"`
	EndpointPort    types.Int64  `tfsdk:"endpoint_port"`
	PublicIPAddress types.String `tfsdk:"public_ip_address"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscore,
// hyphen, and space, max 100 chars (length enforced separately).
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_\- ]+$`)

func (r *bastionResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_bastion"
}

func (r *bastionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud bastion — a managed secure SSH access appliance that " +
			"fronts the private instances of one or more VPCs. Operators reach their otherwise-unreachable " +
			"private hosts through a single, audited entry point instead of exposing every instance to the " +
			"public internet. The CETIC Cloud API has no update endpoint, so any settable attribute " +
			"(`name`, `region`, `plan`, `vpc_id`, `vpc_ids`, `public_ip_id`, `tags`) forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the bastion.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name (max 100 chars; alphanumerics, `_`, `-`, and spaces). " +
					"Immutable — changing forces replacement.",
				Required: true,
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
			"region": schema.StringAttribute{
				MarkdownDescription: "Region code the bastion is provisioned in (e.g. `RNN`). " +
					"Immutable — changing forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "Sizing plan: `small`, `medium`, or `large` (defaults to `small`). " +
					"Immutable — changing forces replacement.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("small"),
				Validators: []validator.String{
					stringvalidator.OneOf("small", "medium", "large"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the primary VPC whose private instances the bastion grants SSH " +
					"access to. Immutable — changing forces replacement. For multi-VPC bastions, add the extra " +
					"VPCs through `vpc_ids` (this primary VPC is always included).",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"vpc_ids": schema.ListAttribute{
				MarkdownDescription: "UUIDs of all the VPCs the bastion fronts (1–5). The primary `vpc_id` is " +
					"always part of the set — list it explicitly to control ordering, or add only the extra " +
					"VPCs. If omitted, the bastion covers just `vpc_id`. Immutable — changing forces replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"public_ip_id": schema.StringAttribute{
				MarkdownDescription: "UUID of a reserved public IP to attach to the bastion endpoint. " +
					"If omitted, the platform allocates one (IPaaS). Immutable — changing forces replacement.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the bastion. The API has no endpoint to " +
					"mutate tags after creation, so changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"endpoint_host": schema.StringAttribute{
				MarkdownDescription: "Public SSH endpoint hostname (or IP) clients connect to. " +
					"Populated once the appliance finishes provisioning.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"endpoint_port": schema.Int64Attribute{
				MarkdownDescription: "TCP port of the SSH endpoint. Populated once the appliance finishes provisioning.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"public_ip_address": schema.StringAttribute{
				MarkdownDescription: "Public IP address attached to the bastion endpoint. " +
					"Populated once the appliance finishes provisioning.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Lifecycle status: `provisioning`, `active`, `error`, or `deleting`. " +
					"Read-only and volatile.",
				Computed: true,
			},
		},
	}
}

func (r *bastionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// applyToModel maps an API Bastion onto the Terraform model. `status` is
// volatile (known-after-apply) so it carries no UseStateForUnknown — see
// CLAUDE.md (v2.0.4 fix). The endpoint fields are nullable until the
// appliance finishes provisioning. Optional+Computed fields that the API may
// omit from its response are preserved on the model rather than blindly
// overwritten with a zero value (CLAUDE.md pitfall #5).
func applyToModel(ctx context.Context, m *bastionResourceModel, b *client.Bastion) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(b.ID)
	m.Name = types.StringValue(b.Name)
	m.Region = types.StringValue(b.Region)
	m.Plan = types.StringValue(b.Plan)
	m.VpcID = types.StringValue(b.VpcID)
	m.Status = types.StringValue(b.Status)

	// vpc_ids: prefer the list; fall back to the single vpc_id for older
	// responses. Preserve the existing value if the API returns neither.
	ids := b.VpcIDs
	if len(ids) == 0 && b.VpcID != "" {
		ids = []string{b.VpcID}
	}
	if len(ids) > 0 || m.VpcIDs.IsNull() || m.VpcIDs.IsUnknown() {
		vpcList, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		if diags.HasError() {
			return diags
		}
		m.VpcIDs = vpcList
	}

	if b.PublicIPID != nil {
		m.PublicIPID = types.StringValue(*b.PublicIPID)
	} else if m.PublicIPID.IsUnknown() {
		m.PublicIPID = types.StringNull()
	}

	tagValues := make([]string, 0, len(b.Tags))
	tagValues = append(tagValues, b.Tags...)
	tagsList, d := types.ListValueFrom(ctx, types.StringType, tagValues)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	m.Tags = tagsList

	if b.EndpointHost != nil {
		m.EndpointHost = types.StringValue(*b.EndpointHost)
	} else {
		m.EndpointHost = types.StringNull()
	}
	if b.EndpointPort != nil {
		m.EndpointPort = types.Int64Value(int64(*b.EndpointPort))
	} else {
		m.EndpointPort = types.Int64Null()
	}
	if b.PublicIPAddress != nil {
		m.PublicIPAddress = types.StringValue(*b.PublicIPAddress)
	} else {
		m.PublicIPAddress = types.StringNull()
	}
	return diags
}

// listToStrings converts a framework List into a Go slice. Null and unknown
// both collapse to nil so callers can hand the result straight to the client.
func listToStrings(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	out := make([]string, 0, len(list.Elements()))
	diags := list.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		return nil, diags
	}
	return out, diags
}

// optStr returns a *string for an Optional value, nil when null/unknown so the
// API applies its own default.
func optStr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func (r *bastionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bastionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vpcIDs, diags := listToStrings(ctx, plan.VpcIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	tags, diags := listToStrings(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateBastion(ctx, client.BastionCreateRequest{
		Name:       plan.Name.ValueString(),
		Region:     plan.Region.ValueString(),
		Plan:       plan.Plan.ValueString(),
		VpcID:      plan.VpcID.ValueString(),
		VpcIDs:     vpcIDs,
		PublicIPID: optStr(plan.PublicIPID),
		Tags:       tags,
	})
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Bastion already exists",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create bastion",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &plan, created)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bastionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bastionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetBastion(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: bastion was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read bastion",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	resp.Diagnostics.Append(applyToModel(ctx, &state, got)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: every mutable field has RequiresReplace, so the framework
// will never call this. Guard with a diagnostic in case someone changes the
// schema later without revisiting Update.
func (r *bastionResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"ccp_bastion has no mutable attributes; all changes force replacement. "+
			"Reaching Update means the schema and the implementation are out of sync — please report this as a provider bug.",
	)
}

func (r *bastionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bastionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteBastion(ctx, state.ID.ValueString()); err != nil {
		// Treat "already gone" as success — no point erroring on destroy when
		// the desired end state is already reached.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete bastion",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Teardown is asynchronous: wait until the appliance is really gone so a
	// replace (destroy-then-create with the same name) doesn't race a 409.
	if err := client.PollUntilDeleted(ctx, 15*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetBastion(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm bastion deletion", err.Error())
	}
}

// ImportState lets users adopt an existing bastion with `terraform import
// ccp_bastion.example <uuid>`. Read fills the rest.
func (r *bastionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
