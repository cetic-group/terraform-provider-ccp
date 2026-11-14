package objectbucket

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Versioning of a bucket (cetic-cloud-platform#1923).
//
// Like `cors_rule`, the `versioning` block is opt-in: without it the resource
// does not manage versioning and leaves a setting made from the console or the
// CLI untouched. Versioning is never disabled — S3 cannot go back to "never
// versioned", a versioned bucket is suspended — so removing the block only
// stops managing it; the plan says so.

// Bounds mirrored from the platform so that a refused value is reported at plan time.
const (
	versioningMinDays = 1
	versioningMaxDays = 3650
)

// versioningAttrTypes is the object type of the `versioning` block.
var versioningAttrTypes = map[string]attr.Type{
	"status":                             types.StringType,
	"noncurrent_version_expiration_days": types.Int64Type,
}

// versioningModel mirrors the `versioning` block.
type versioningModel struct {
	Status                          types.String `tfsdk:"status"`
	NoncurrentVersionExpirationDays types.Int64  `tfsdk:"noncurrent_version_expiration_days"`
}

func versioningBlock() schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		MarkdownDescription: "Versioning of the bucket: every overwrite or deletion keeps the previous " +
			"version of the object, which is deleted after `noncurrent_version_expiration_days`. " +
			"Without this block, versioning is not managed by this resource and a setting made " +
			"from the console or the CLI is left untouched. Versioning is never disabled, only " +
			"suspended: removing the block stops managing it and leaves the bucket as it is.",
		// ⚠️ `status` is required by the BLOCK, not by the attribute: a Required
		// attribute inside a single nested block is enforced even when the block is
		// absent, which would refuse every bucket configured without versioning.
		Validators: []validator.Object{
			objectvalidator.AlsoRequires(path.MatchRelative().AtName("status")),
		},
		Attributes: map[string]schema.Attribute{
			"status": schema.StringAttribute{
				MarkdownDescription: "(Required in the block) `Enabled` or `Suspended`. A suspended " +
					"bucket stops creating versions and keeps the existing ones. A new bucket cannot " +
					"be created suspended.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.OneOf("Enabled", "Suspended")},
			},
			"noncurrent_version_expiration_days": schema.Int64Attribute{
				MarkdownDescription: "Days a previous version is kept before being deleted, " +
					"1 to 3650. When unset, the platform keeps the current value (30 days for a " +
					"bucket never versioned). Shortening it deletes previous versions sooner. A " +
					"lifecycle rule of your own that keeps them longer makes the platform refuse " +
					"a shorter value.",
				Optional:   true,
				Computed:   true,
				Validators: []validator.Int64{int64validator.Between(versioningMinDays, versioningMaxDays)},
				PlanModifiers: []planmodifier.Int64{
					int64UseStateForUnknown(),
				},
			},
		},
	}
}

func nullVersioning() types.Object {
	return types.ObjectNull(versioningAttrTypes)
}

// versioningFromObject returns the block, or nil when it is absent or unknown.
func versioningFromObject(ctx context.Context, o types.Object) (*versioningModel, diag.Diagnostics) {
	if o.IsNull() || o.IsUnknown() {
		return nil, nil
	}
	var m versioningModel
	diags := o.As(ctx, &m, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return nil, diags
	}
	return &m, diags
}

func versioningToObject(status types.String, days types.Int64) types.Object {
	return types.ObjectValueMust(versioningAttrTypes, map[string]attr.Value{
		"status":                             status,
		"noncurrent_version_expiration_days": days,
	})
}

// versioningFromAPI projects the platform's answer onto the block.
func versioningFromAPI(v *client.BucketVersioning) types.Object {
	status := types.StringNull()
	if v.Status != nil {
		status = types.StringValue(*v.Status)
	}
	days := types.Int64Null()
	if v.NoncurrentVersionExpirationDays != nil {
		days = types.Int64Value(*v.NoncurrentVersionExpirationDays)
	}
	return versioningToObject(status, days)
}

// versioningRequest builds the write. The retention is only sent when the
// practitioner set it: otherwise the platform keeps the value in place.
func versioningRequest(m *versioningModel) client.BucketVersioningUpdateRequest {
	req := client.BucketVersioningUpdateRequest{Status: m.Status.ValueString()}
	if !m.NoncurrentVersionExpirationDays.IsNull() && !m.NoncurrentVersionExpirationDays.IsUnknown() {
		d := m.NoncurrentVersionExpirationDays.ValueInt64()
		req.NoncurrentVersionExpirationDays = &d
	}
	return req
}

// versioningNeedsWrite reports whether the planned block differs from the state.
// An unset retention (unknown in the plan) is not a change.
func versioningNeedsWrite(plan, state *versioningModel) bool {
	if plan == nil {
		return false
	}
	if state == nil || !plan.Status.Equal(state.Status) {
		return true
	}
	days := plan.NoncurrentVersionExpirationDays
	return !days.IsNull() && !days.IsUnknown() && !days.Equal(state.NoncurrentVersionExpirationDays)
}

// keptVersioning is the block recorded when nothing was written: the planned
// status and the retention known so far. A computed attribute must never stay
// unknown after apply.
func keptVersioning(plan, state *versioningModel) types.Object {
	days := plan.NoncurrentVersionExpirationDays
	if days.IsUnknown() {
		days = types.Int64Null()
		if state != nil && !state.NoncurrentVersionExpirationDays.IsUnknown() {
			days = state.NoncurrentVersionExpirationDays
		}
	}
	return versioningToObject(plan.Status, days)
}

// applyVersioningOnCreate writes the planned versioning to a freshly created bucket.
//
// ⚠️ Like the CORS rules, a refusal must NOT be returned as an error: Terraform
// would mark the bucket tainted and replace it on the next apply. The planned
// block is recorded with a warning; the next refresh reads the real versioning,
// so the next plan shows what is still to apply.
func (r *objectBucketResource) applyVersioningOnCreate(ctx context.Context, id string, plan *objectBucketResourceModel) diag.Diagnostics {
	m, diags := versioningFromObject(ctx, plan.Versioning)
	if diags.HasError() || m == nil {
		return diags
	}
	out, err := r.client.PutBucketVersioning(ctx, id, versioningRequest(m))
	if err != nil {
		plan.Versioning = keptVersioning(m, nil)
		diags.AddWarning("Versioning was not applied",
			fmt.Sprintf("Bucket %s was created, but the platform refused its versioning: %s\n\n"+
				"The bucket is kept (it is not replaced). The next `terraform apply` reads the real "+
				"versioning and applies the `versioning` block again.", id, err.Error()))
		return diags
	}
	plan.Versioning = versioningFromAPI(out)
	return diags
}

// versioningStatusWarning reports a versioning that is saved but not applied.
func versioningStatusWarning(id string, v *client.BucketVersioning) (string, string, bool) {
	switch v.SyncStatus {
	case "error":
		msg := "the platform could not apply it"
		if v.ErrorMessage != nil && *v.ErrorMessage != "" {
			msg = *v.ErrorMessage
		}
		return "Versioning is saved but not applied",
			fmt.Sprintf("Bucket %s: %s. The platform retries automatically.", id, msg), true
	case "pending":
		return "Versioning is still being applied",
			fmt.Sprintf("Bucket %s: the versioning is saved and not yet applied to the bucket.", id), true
	}
	return "", "", false
}
