// Package objectbucket implements the ccp_object_bucket Terraform
// resource.
//
// An object bucket in CETIC Cloud is a Ceph RGW S3 bucket owned by a
// per-tenant RGW user. Provisioning and deletion are asynchronous: the API
// returns 201/202 with a transient status (`creating`, `deleting`) and a
// Celery worker performs the radosgw-admin / S3 operations. The provider
// therefore polls GetObjectBucket after creation until the bucket reaches a
// quiescent state.
//
// Mutability matrix (handled in Update):
//
//   - name        RequiresReplace (S3 bucket names are immutable)
//   - region      RequiresReplace
//   - tags        RequiresReplace (no PATCH endpoint for tags)
//   - is_public   mutable in place (synchronous PATCH)
//
// Credentials (`access_key`, `secret_key`) are fetched via the
// `/credentials` endpoint after the bucket reaches `active` and stored in
// Terraform state. They are tenant-region-wide master credentials: every
// bucket of the same (tenant, region) shares them, and the API does not
// rotate them today.
package objectbucket

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time interface assertions.
var (
	_ resource.Resource                = (*objectBucketResource)(nil)
	_ resource.ResourceWithConfigure   = (*objectBucketResource)(nil)
	_ resource.ResourceWithImportState = (*objectBucketResource)(nil)
)

// New returns a freshly-constructed ccp_object_bucket resource.
// Wired in by provider.go via objectbucket.New.
func New() resource.Resource {
	return &objectBucketResource{}
}

// objectBucketResource is the framework Resource implementation. The client
// is stashed in Configure and reused by Create/Read/Update/Delete.
type objectBucketResource struct {
	client *client.Client
}

// objectBucketResourceModel mirrors the schema below 1-to-1. Tag names must
// match the schema attribute keys exactly.
type objectBucketResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Region       types.String `tfsdk:"region"`
	IsPublic     types.Bool   `tfsdk:"is_public"`
	Tags         types.List   `tfsdk:"tags"`
	EndpointURL  types.String `tfsdk:"endpoint_url"`
	SizeBytes    types.Int64  `tfsdk:"size_bytes"`
	Status       types.String `tfsdk:"status"`
	ErrorMessage types.String `tfsdk:"error_message"`
	AccessKey    types.String `tfsdk:"access_key"`
	SecretKey    types.String `tfsdk:"secret_key"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

// bucketNamePattern matches the API constraint: S3-compliant DNS names,
// lowercase alphanumerics and hyphens, must start and end with an
// alphanumeric, length 3..63.
var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{1,61}[a-z0-9]$`)

// Polling parameters.
//
//   - createPollTimeout: time we wait for `creating` → `active`.
//   - deletePollTimeout: time we wait for the API to start returning 404.
const (
	pollInterval      = 5 * time.Second
	createPollTimeout = 90 * time.Second
	deletePollTimeout = 120 * time.Second
)

func (r *objectBucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_bucket"
}

func (r *objectBucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CETIC Cloud object bucket (Ceph RGW S3). " +
			"Buckets are owned by a per-tenant RGW user; their S3 master " +
			"credentials are exposed as `access_key` and `secret_key` and are " +
			"shared by every bucket of the same `(tenant, region)`. Bucket " +
			"names must follow S3 DNS rules (lowercase, 3–63 chars). The " +
			"`is_public` flag is mutable in place; `name`, `region`, and " +
			"`tags` force replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the bucket.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Bucket name. Must follow S3 DNS rules: " +
					"lowercase letters, digits and hyphens; must start and end " +
					"with an alphanumeric; 3–63 characters. Bucket names are " +
					"immutable, so changes force replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 63),
					stringvalidator.RegexMatches(
						bucketNamePattern,
						"must be 3–63 chars, lowercase alphanumerics or hyphens, "+
							"starting and ending with an alphanumeric",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "Cloud Lake region. One of `RNN` (Rennes, France), " +
					"`PAR` (Paris, France), or `ABJ` (Abidjan, Côte d'Ivoire).",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("RNN", "PAR", "ABJ"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"is_public": schema.BoolAttribute{
				MarkdownDescription: "When true, the bucket policy grants anonymous " +
					"read access to its objects. Mutable in place via the API's " +
					"PATCH endpoint.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Free-form labels attached to the bucket. The " +
					"API has no endpoint to mutate tags after creation, so " +
					"changes here force replacement.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"endpoint_url": schema.StringAttribute{
				MarkdownDescription: "Public S3 endpoint URL for the bucket's region " +
					"(e.g. `https://s3.in.techledger.io`). Null until the bucket " +
					"reaches `active`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"size_bytes": schema.Int64Attribute{
				MarkdownDescription: "Total size of the objects stored in the bucket, " +
					"in bytes. Reported by RGW usage statistics.",
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle state. One of `creating`, " +
					"`active`, `deleting`, or `error`.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"error_message": schema.StringAttribute{
				MarkdownDescription: "Last error message reported by the provisioner, if any.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"access_key": schema.StringAttribute{
				MarkdownDescription: "S3 access key for this bucket's tenant in the " +
					"region. Tenant-region-wide master key — every bucket of the " +
					"same `(tenant, region)` shares it. Sensitive.",
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret_key": schema.StringAttribute{
				MarkdownDescription: "S3 secret key paired with `access_key`. Sensitive.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp at which the bucket was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last server-side update.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *objectBucketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *objectBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectBucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tags, diags := stringsFromList(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.ObjectBucketCreateRequest{
		Name:     plan.Name.ValueString(),
		Region:   plan.Region.ValueString(),
		IsPublic: plan.IsPublic.ValueBool(),
		Tags:     tags,
	}

	created, err := r.client.CreateObjectBucket(ctx, createReq)
	if err != nil {
		switch {
		case client.IsConflict(err):
			resp.Diagnostics.AddError(
				"Object bucket name already exists in this region",
				fmt.Sprintf("Cloud Lake rejected the create call with a 409 conflict. "+
					"Bucket names must be unique within a region. Underlying error: %s",
					err.Error()),
			)
			return
		default:
			// 422 (invalid bucket name per S3 rules) and other validation
			// errors land here — surface the API detail directly.
			resp.Diagnostics.AddError(
				"Failed to create object bucket",
				fmt.Sprintf("Cloud Lake API error: %s", err.Error()),
			)
			return
		}
	}

	// Wait for `creating` → `active`. An immediate `error` short-circuits.
	if err := pollForActive(ctx, r.client, created.ID, createPollTimeout); err != nil {
		resp.Diagnostics.AddError(
			"Object bucket failed to reach active state",
			fmt.Sprintf("Bucket %s did not reach `active` within %s: %s",
				created.ID, createPollTimeout, err.Error()),
		)
		return
	}

	// Re-fetch the authoritative record so endpoint_url, size_bytes,
	// timestamps and tags echo what the API has on file.
	fresh, err := r.client.GetObjectBucket(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read object bucket after provisioning",
			fmt.Sprintf("Bucket %s was created but the follow-up GET failed: %s",
				created.ID, err.Error()),
		)
		return
	}

	diags = applyBucketToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Fetch credentials. Bucket is `active` per the poll above so the API
	// should hand them out; if it 409s we leave them blank with a warning so
	// the user can re-apply later.
	if err := r.refreshCredentials(ctx, &plan, fresh); err != nil {
		resp.Diagnostics.AddWarning(
			"Object bucket credentials not available",
			fmt.Sprintf("Bucket %s is active but the credentials endpoint returned: %s. "+
				"Re-run `terraform apply` once provisioning settles to populate "+
				"`access_key` and `secret_key`.", created.ID, err.Error()),
		)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetObjectBucket(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Standard drift handling: bucket was deleted out-of-band.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to read object bucket",
			fmt.Sprintf("Cloud Lake API error for id %s: %s",
				state.ID.ValueString(), err.Error()),
		)
		return
	}

	// `deleting` is a transient state we leave in state as-is — the next
	// refresh will pick up the eventual 404 and remove the resource.
	if got.Status == client.BucketStatusDeleting {
		state.Status = types.StringValue(got.Status)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	diags := applyBucketToModel(ctx, got, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Refresh credentials only when the bucket is `active`. In any other
	// state the credentials endpoint is likely to 409, and we'd rather keep
	// whatever was already in state than blank it out.
	if got.Status == client.BucketStatusActive {
		if err := r.refreshCredentials(ctx, &state, got); err != nil {
			// Soft failure: keep prior creds, surface a warning.
			resp.Diagnostics.AddWarning(
				"Could not refresh object bucket credentials",
				fmt.Sprintf("Bucket %s is active but the credentials endpoint "+
					"returned: %s. Existing `access_key` / `secret_key` in state "+
					"are left untouched.", got.ID, err.Error()),
			)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update handles the single mutable axis — is_public — via the synchronous
// PATCH endpoint. Any tags drift would have triggered RequiresReplace; if
// it somehow gets here, we surface a defensive diagnostic.
func (r *objectBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state objectBucketResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Defensive check: tags should have triggered RequiresReplace before we
	// ever reach Update. If we still see a divergence, refuse rather than
	// silently dropping the change.
	if !plan.Tags.Equal(state.Tags) {
		resp.Diagnostics.AddError(
			"Object bucket tags cannot be updated in place",
			"The Cloud Lake API does not expose an endpoint to mutate bucket "+
				"tags. Changing `tags` should force replacement; reaching this "+
				"branch indicates a provider bug — please report it.",
		)
		return
	}

	// is_public — synchronous PATCH.
	if !plan.IsPublic.Equal(state.IsPublic) {
		newVal := plan.IsPublic.ValueBool()
		if _, err := r.client.UpdateObjectBucket(ctx, id, client.ObjectBucketUpdateRequest{
			IsPublic: &newVal,
		}); err != nil {
			if client.IsConflict(err) {
				resp.Diagnostics.AddError(
					"Object bucket update conflicts with current state",
					fmt.Sprintf("Cloud Lake rejected the PATCH for bucket %s: %s",
						id, err.Error()),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Failed to update object bucket",
				fmt.Sprintf("Cloud Lake API error for id %s: %s", id, err.Error()),
			)
			return
		}
	}

	// Re-fetch and project onto state.
	fresh, err := r.client.GetObjectBucket(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to read object bucket after update",
			fmt.Sprintf("Bucket %s was updated but the follow-up GET failed: %s",
				id, err.Error()),
		)
		return
	}

	diags := applyBucketToModel(ctx, fresh, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Carry forward credentials from prior state — they don't rotate on
	// update — but refresh opportunistically if the bucket is still active.
	plan.AccessKey = state.AccessKey
	plan.SecretKey = state.SecretKey
	if fresh.Status == client.BucketStatusActive {
		_ = r.refreshCredentials(ctx, &plan, fresh) // soft failure, ignore
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectBucketResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if err := r.client.DeleteObjectBucket(ctx, id); err != nil {
		// Treat "already gone" as success.
		if client.IsNotFound(err) {
			return
		}
		if client.IsConflict(err) {
			resp.Diagnostics.AddError(
				"Object bucket deletion conflicts with current state",
				fmt.Sprintf("Cloud Lake refused to delete bucket %s: %s", id, err.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Failed to delete object bucket",
			fmt.Sprintf("Cloud Lake API error for id %s: %s", id, err.Error()),
		)
		return
	}

	// Poll for 404. If we time out, warn but let Terraform drop the resource;
	// the backend is converging asynchronously.
	pollErr := client.Poll(ctx, pollInterval, deletePollTimeout, func(ctx context.Context) (bool, error) {
		_, err := r.client.GetObjectBucket(ctx, id)
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
			"Object bucket deletion did not complete within the timeout",
			fmt.Sprintf("Bucket %s was scheduled for deletion but did not disappear "+
				"within %s: %s. Terraform will remove the resource from state; the "+
				"Cloud Lake backend should finish the teardown asynchronously.",
				id, deletePollTimeout, pollErr.Error()),
		)
	}
}

// ImportState lets users adopt an existing bucket with `terraform import
// ccp_object_bucket.example <uuid>`. Read fills the rest, including
// credentials when the bucket is active.
func (r *objectBucketResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// refreshCredentials calls GET /v1/buckets/{id}/credentials and patches the
// `access_key` / `secret_key` fields of dst. Returns an error if the call
// fails so the caller can choose to surface a warning vs. abort. When src
// is non-active the call is skipped and the function returns nil — callers
// that care should pre-check src.Status.
func (r *objectBucketResource) refreshCredentials(ctx context.Context, dst *objectBucketResourceModel, src *client.ObjectBucket) error {
	if src.Status != client.BucketStatusActive {
		return nil
	}
	creds, err := r.client.GetObjectBucketCredentials(ctx, src.ID)
	if err != nil {
		return err
	}
	dst.AccessKey = types.StringValue(creds.AccessKey)
	dst.SecretKey = types.StringValue(creds.SecretKey)
	// The credentials response carries a definitive endpoint URL — prefer
	// it over the bucket record's nullable one when populated.
	if creds.EndpointURL != "" {
		dst.EndpointURL = types.StringValue(creds.EndpointURL)
	}
	return nil
}

// pollForActive polls GetObjectBucket every pollInterval up to timeout,
// stopping when the bucket reaches `active`. Reaching `error` is treated as
// a hard failure and surfaces error_message. The 404 case is also a hard
// failure here — the caller should not be polling for a deleted bucket.
func pollForActive(ctx context.Context, c *client.Client, id string, timeout time.Duration) error {
	return client.Poll(ctx, pollInterval, timeout, func(ctx context.Context) (bool, error) {
		cur, err := c.GetObjectBucket(ctx, id)
		if err != nil {
			return false, err
		}
		switch cur.Status {
		case client.BucketStatusActive:
			return true, nil
		case client.BucketStatusError:
			msg := ""
			if cur.ErrorMessage != nil {
				msg = *cur.ErrorMessage
			}
			return false, fmt.Errorf("bucket %s entered error state: %s", cur.ID, msg)
		default:
			return false, nil
		}
	})
}

// applyBucketToModel populates the framework model from the API
// representation. Always called after a successful Create/Read/Update so
// state reflects the authoritative server view. Tags are normalised so a
// `nil` API response and an empty list both produce an empty list in state
// (avoids spurious diffs against an Optional+Computed list attribute).
//
// `access_key` / `secret_key` are deliberately NOT touched here — they are
// fetched from a separate endpoint by refreshCredentials.
func applyBucketToModel(ctx context.Context, src *client.ObjectBucket, dst *objectBucketResourceModel) diag.Diagnostics {
	dst.ID = types.StringValue(src.ID)
	dst.Name = types.StringValue(src.Name)
	dst.Region = types.StringValue(src.Region)
	dst.IsPublic = types.BoolValue(src.IsPublic)
	dst.SizeBytes = types.Int64Value(src.SizeBytes)
	dst.Status = types.StringValue(src.Status)
	dst.EndpointURL = stringPtrToValue(src.EndpointURL)
	dst.ErrorMessage = stringPtrToValue(src.ErrorMessage)
	dst.CreatedAt = types.StringValue(src.CreatedAt.Format(time.RFC3339))
	dst.UpdatedAt = types.StringValue(src.UpdatedAt.Format(time.RFC3339))

	tagValues := make([]string, 0, len(src.Tags))
	tagValues = append(tagValues, src.Tags...)
	tagsList, diags := types.ListValueFrom(ctx, types.StringType, tagValues)
	if diags.HasError() {
		return diags
	}
	dst.Tags = tagsList
	return diags
}

// stringPtrToValue collapses a *string into a framework String value: nil →
// Null, otherwise the underlying string.
func stringPtrToValue(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// stringsFromList converts the framework List representation into a Go
// slice. Null and unknown both collapse to nil so callers can hand the
// result straight to the API client.
func stringsFromList(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
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

// int64UseStateForUnknown is a tiny helper that returns a planmodifier.Int64
// equivalent to stringplanmodifier.UseStateForUnknown — the framework does
// not expose one for Int64 in older versions of the helpers, so we wrap the
// modifier inline. With v1.13+ the dedicated helper is available; using a
// thin shim keeps this file self-contained and compatible across the
// minimum supported framework versions.
func int64UseStateForUnknown() planmodifier.Int64 {
	return useStateForUnknownInt64Modifier{}
}

type useStateForUnknownInt64Modifier struct{}

func (m useStateForUnknownInt64Modifier) Description(_ context.Context) string {
	return "Once set, the value of this attribute in state will not change."
}

func (m useStateForUnknownInt64Modifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m useStateForUnknownInt64Modifier) PlanModifyInt64(_ context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	// Do nothing if there is no state value (resource being created).
	if req.StateValue.IsNull() {
		return
	}
	// Do nothing if the plan value is known (already set by config or other
	// modifier).
	if !req.PlanValue.IsUnknown() {
		return
	}
	// Do nothing if there is a known config value.
	if !req.ConfigValue.IsNull() {
		return
	}
	resp.PlanValue = req.StateValue
}
