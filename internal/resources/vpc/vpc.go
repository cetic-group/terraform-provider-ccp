// Package vpc implements the ccp_vpc Terraform resource.
//
// A VPC in CETIC Cloud is a Proxmox SDN zone (vxlan, with a per-VPC NAT GW LXC
// provisioned lazily on first VNet creation). The API exposes no PATCH
// endpoint, so every user-settable attribute (`name`, `region`, `tags`) forces
// replacement on change.
//
// Provisioning is asynchronous: POST /v1/vpcs returns 201 immediately but the
// SDN zone may still be propagating across nodes. Status transitions are
// `active` (terminal success) or `error` (terminal failure). We poll
// GetVPC for up to 90 s and re-fetch the resource once it settles so the
// Terraform state reflects the authoritative server view (vlan_id, sdn_type,
// timestamps).
//
// Deletion is also asynchronous: the VPC enters `deleting` and disappears
// from the API once the NAT GW teardown + zone removal completes. We poll
// for 404 up to 60 s and surface a warning rather than an error if the
// timeout elapses, since the resource will still be removed from Terraform
// state and the API is converging on its own.
package vpc

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*vpcResource)(nil)
	_ resource.ResourceWithConfigure   = (*vpcResource)(nil)
	_ resource.ResourceWithImportState = (*vpcResource)(nil)
)

// New returns a freshly-constructed ccp_vpc resource. Wired in by
// provider.go via vpc.New.
func New() resource.Resource {
	return &vpcResource{}
}

// vpcResource is the framework Resource implementation. The client is stashed
// in Configure and reused by Create/Read/Update/Delete.
type vpcResource struct {
	client *client.Client
}

// vpcResourceModel mirrors the schema below 1-to-1. Tag names must match the
// schema attribute keys exactly.
type vpcResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Region    types.String `tfsdk:"region"`
	Tags      types.List   `tfsdk:"tags"`
	VlanID    types.Int64  `tfsdk:"vlan_id"`
	SDNType   types.String `tfsdk:"sdn_type"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`
}

// nameValidatorPattern matches the API constraint: alphanumerics, underscores
// and hyphens (length enforced separately).
var nameValidatorPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Polling parameters — Create waits up to 90 s for the VPC to leave the
// transitional state, Delete waits up to 60 s for the resource to disappear.
const (
	createPollInterval = 5 * time.Second
	createPollTimeout  = 90 * time.Second
	deletePollInterval = 5 * time.Second
	deletePollTimeout  = 60 * time.Second
)

func (r *vpcResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_vpc"
}

func (r *vpcResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud VPC. A VPC is a regional layer-3 boundary " +
			"(backed by a Proxmox SDN VXLAN zone and a per-VPC NAT gateway). The API has no " +
			"in-place update endpoint, so any change to `name`, `region`, or `tags` forces " +
			"replacement. Creation is asynchronous: the provider polls until the VPC reaches " +
			"the `active` state (or up to 90 seconds).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the VPC.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "VPC name (max 100 chars; alphanumerics, `_`, and `-`).",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(100),
					stringvalidator.RegexMatches(
						nameValidatorPattern,
						"must contain only letters, digits, underscores, or hyphens",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "CETIC Cloud region. One of `RNN` (Rennes, France), " +
					"`PAR` (Paris, France), or `ABJ` (Abidjan, Côte d'Ivoire).",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("RNN", "PAR", "ABJ"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the VPC. The API has no " +
					"endpoint to mutate tags after creation, so changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"vlan_id": schema.Int64Attribute{
				MarkdownDescription: "VLAN tag assigned by Proxmox SDN. Allocated by the API " +
					"from the per-tenant pool (100–3999); not user-settable.",
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"sdn_type": schema.StringAttribute{
				MarkdownDescription: "Backing SDN zone type. `vxlan` for VPCs created today, " +
					"`simple` for pre-2026-04-19 legacy VPCs.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle state. One of `active`, `deleting`, or " +
					"`error`. After a successful apply this will always be `active`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the VPC was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *vpcResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *vpcResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan vpcResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Materialise tags from the framework List into a plain []string. A null
	// or unknown list collapses to nil, which the API treats as "no tags".
	tags, diags := tagsFromList(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateVPC(ctx, client.VPCCreateRequest{
		Name:   plan.Name.ValueString(),
		Region: plan.Region.ValueString(),
		Tags:   tags,
	})
	if err != nil {
		// 409 (e.g. name collision) and 503 (no active Proxmox node in the
		// region) both arrive with a detail message we want to surface
		// verbatim — the API speaks French and the messages are user-facing.
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"VPC creation conflicts with an existing resource",
				fmt.Sprintf("CETIC Cloud rejected the create call: %s", err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to create VPC",
			fmt.Sprintf("CETIC Cloud API error: %s", err.Error()),
		)
		return
	}

	// Fast path: the API may already report `active` on the initial response.
	// Otherwise poll until the status settles.
	final := created
	switch created.Status {
	case client.StatusActive:
		// Done — no extra round-trip needed.
	case client.StatusError:
		resp.Diagnostics.AddError(
			"VPC entered error state during provisioning",
			fmt.Sprintf("VPC %s reported status `error` immediately after creation. "+
				"Check the CETIC Cloud console or backoffice for the underlying cause.", created.ID),
		)
		return
	default:
		pollErr := client.Poll(ctx, createPollInterval, createPollTimeout, func(ctx context.Context) (bool, error) {
			cur, err := r.client.GetVPC(ctx, created.ID)
			if err != nil {
				return false, err
			}
			switch cur.Status {
			case client.StatusError:
				return false, fmt.Errorf("VPC %s entered error state during provisioning", cur.ID)
			case client.StatusActive:
				return true, nil
			default:
				return false, nil
			}
		})
		if pollErr != nil {
			resp.Diagnostics.AddError(
				"VPC failed to reach active state",
				fmt.Sprintf("CETIC Cloud VPC %s did not become active within %s: %s",
					created.ID, createPollTimeout, pollErr.Error()),
			)
			return
		}
		// Re-fetch the authoritative record after polling — the initial
		// response may not reflect the final vlan_id / sdn_type / tags.
		fresh, err := r.client.GetVPC(ctx, created.ID)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to read VPC after provisioning",
				fmt.Sprintf("VPC %s reached active state but the follow-up GET failed: %s",
					created.ID, err.Error()),
			)
			return
		}
		final = fresh
	}

	diags = applyVPCToModel(ctx, final, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *vpcResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state vpcResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetVPC(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: VPC was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read VPC",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we still surface verbatim in state —
	// the next plan/apply will pick up the eventual 404 and remove it.
	diags := applyVPCToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op: every user-settable attribute carries RequiresReplace,
// so the framework will replace the resource via destroy + create rather than
// call Update. We still emit a diagnostic if it is somehow invoked, since
// reaching this branch would mean the schema and implementation drifted.
func (r *vpcResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"VPC has no in-place updates",
		"ccp_vpc has no mutable attributes; all changes force replacement. "+
			"This should not happen with all-RequiresReplace fields — please report this as a provider bug.",
	)
}

func (r *vpcResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state vpcResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.DeleteVPC(ctx, id); err != nil {
		// Treat "already gone" as success — no point erroring on destroy when
		// the desired end state is already reached.
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete VPC",
			fmt.Sprintf("CETIC Cloud API error for id %s: %s", id, err.Error()),
		)
		return
	}

	// Poll until GetVPC returns 404. If the timeout elapses, warn but let
	// Terraform remove the resource from state — CETIC Cloud is still
	// converging asynchronously and blocking the apply would be worse.
	pollErr := client.Poll(ctx, deletePollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetVPC(ctx, id)
		if err == nil {
			return false, nil
		}
		if client.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	if pollErr != nil {
		resp.Diagnostics.AddWarning(
			"VPC deletion did not complete within the timeout",
			fmt.Sprintf("VPC %s was scheduled for deletion but did not disappear within %s: %s. "+
				"Terraform will remove the resource from state; the CETIC Cloud backend should "+
				"finish the teardown asynchronously.", id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing VPC with `terraform import
// ccp_vpc.example <uuid>`. Read fills the rest.
func (r *vpcResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyVPCToModel populates the framework model from the API representation.
// Always called after a successful Create/Read so state reflects the
// authoritative server view. Tags are normalised so a `nil` API response and
// an empty list both produce an empty list in state (avoids spurious diffs
// against an Optional+Computed list attribute).
func applyVPCToModel(ctx context.Context, src *client.VPC, dst *vpcResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.Name = types.StringValue(src.Name)
	dst.Region = types.StringValue(src.Region)
	dst.SDNType = types.StringValue(src.SDNType)
	dst.Status = types.StringValue(src.Status)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))

	if src.VlanID != nil {
		dst.VlanID = types.Int64Value(int64(*src.VlanID))
	} else {
		dst.VlanID = types.Int64Null()
	}

	tagValues := make([]string, 0, len(src.Tags))
	tagValues = append(tagValues, src.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	dst.Tags = tagsList
	return diags
}

// tagsFromList converts the framework List representation into a Go slice.
// Null and unknown both collapse to nil so callers can hand the result
// straight to the API client.
func tagsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
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
