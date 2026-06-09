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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
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
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Region       types.String `tfsdk:"region"`
	VpcID        types.String `tfsdk:"vpc_id"`
	Status       types.String `tfsdk:"status"`
	EndpointHost types.String `tfsdk:"endpoint_host"`
	EndpointPort types.Int64  `tfsdk:"endpoint_port"`
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
			"fronts the private instances of a VPC. Operators reach their otherwise-unreachable private " +
			"hosts through a single, audited entry point instead of exposing every instance to the public " +
			"internet. The CETIC Cloud API has no update endpoint, so any change to `name`, `region` or " +
			"`vpc_id` forces replacement.",
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
			"vpc_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the VPC whose private instances the bastion grants SSH access to. " +
					"Immutable — changing forces replacement.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
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
// appliance finishes provisioning.
func applyToModel(m *bastionResourceModel, b *client.Bastion) {
	m.ID = types.StringValue(b.ID)
	m.Name = types.StringValue(b.Name)
	m.Region = types.StringValue(b.Region)
	m.VpcID = types.StringValue(b.VpcID)
	m.Status = types.StringValue(b.Status)
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
}

func (r *bastionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bastionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateBastion(ctx, client.BastionCreateRequest{
		Name:   plan.Name.ValueString(),
		Region: plan.Region.ValueString(),
		VpcID:  plan.VpcID.ValueString(),
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

	applyToModel(&plan, created)
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

	applyToModel(&state, got)
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
