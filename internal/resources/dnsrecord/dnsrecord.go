// Package dnsrecord implements the ccp_dns_record resource.
//
// One resource = one **record set**: the (name, type) couple and ALL of its
// values. That is the unit the API edits, and it is the reason for the two
// least obvious choices in this schema:
//
//   - `records` is a `Set`, not a `List`. The API returns the values in order
//     of first appearance, not in the order they were sent, and the values of
//     a record set are unordered by nature. A `List` would show a change at
//     every `terraform plan` after the first reordering, with nothing having
//     moved.
//   - `records` REPLACES. The API rewrites the whole value list on every
//     write, which is exactly Terraform's model: the configuration is the
//     desired truth and the provider sends it whole. There is no delta to
//     compute, and no incremental "add one value".
//
// `name` and `type` identify the record set: there is no route to change them.
// They force replacement, which is the correct gesture — destroy and recreate.
package dnsrecord

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*dnsRecordResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsRecordResource)(nil)
	_ resource.ResourceWithImportState = (*dnsRecordResource)(nil)
)

func New() resource.Resource { return &dnsRecordResource{} }

type dnsRecordResource struct{ client *client.Client }

// recordTypes is deliberately narrower than what the API accepts.
//
// `NS` is left out: the one at the apex is placed by the platform and is read
// only, and anywhere else it is a delegation, which a private zone refuses.
// Offering it would only produce plans the API rejects.
var recordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA"}

// A value is sent as written. Padding it with spaces would be trimmed by the
// API and the value read back would differ from the configured one — which
// Terraform reports as "inconsistent result after apply", far from the cause.
var noSurroundingSpace = regexp.MustCompile(`^\S(.*\S)?$`)

type dnsRecordModel struct {
	ID              types.String `tfsdk:"id"`
	ZoneID          types.String `tfsdk:"zone_id"`
	Name            types.String `tfsdk:"name"`
	Type            types.String `tfsdk:"type"`
	TTL             types.Int64  `tfsdk:"ttl"`
	Records         types.Set    `tfsdk:"records"`
	FQDN            types.String `tfsdk:"fqdn"`
	IsSystemManaged types.Bool   `tfsdk:"is_system_managed"`
	CreatedAt       types.String `tfsdk:"created_at"`
}

func (r *dnsRecordResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_dns_record"
}

func (r *dnsRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one record of a private DNS zone — a name, a type, and all " +
			"the values answered for that pair.\n\n" +
			"~> **`records` is the whole answer, not an addition.** Every apply sends the complete " +
			"set: a value removed from the configuration stops being answered.\n\n" +
			"`name` and `type` identify the record, so changing either one replaces the resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the record.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the zone (`ccp_dns_zone.id`). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the record, relative to the zone (`www`), fully " +
					"qualified (`www.corp.internal`), or `@` for the zone itself. The fully " +
					"qualified form is reported back in `fqdn`. Forces replacement.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Record type: `A`, `AAAA`, `CNAME`, `MX`, `TXT`, `SRV` or " +
					"`CAA`. Forces replacement.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(recordTypes...),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"ttl": schema.Int64Attribute{
				MarkdownDescription: "How long the answer may be cached, in seconds (60 to 604800). " +
					"Defaults to 3600. Changed in place.",
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(3600),
				Validators: []validator.Int64{
					int64validator.Between(60, 604800),
				},
			},
			"records": schema.SetAttribute{
				MarkdownDescription: "The values answered for this name and type, in presentation " +
					"form — `10 mail.example.com.` for a `MX`, `\"v=spf1 -all\"` **with the quotes** " +
					"for a `TXT`. 1 to 32 values.\n\n" +
					"A set, not a list: the values of a record are unordered, and the API returns " +
					"them in its own order. Ordering them would show a change at every plan without " +
					"anything having moved.",
				Required:    true,
				ElementType: types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeBetween(1, 32),
					setvalidator.ValueStringsAre(
						stringvalidator.LengthBetween(1, 4096),
						stringvalidator.RegexMatches(
							noSurroundingSpace,
							"must not start or end with whitespace — the platform trims it, which "+
								"would make the value read back differ from the one configured",
						),
					),
				},
			},
			"fqdn": schema.StringAttribute{
				MarkdownDescription: "Fully qualified name of the record, as the platform stores it.",
				Computed:            true,
			},
			"is_system_managed": schema.BoolAttribute{
				MarkdownDescription: "Whether the record is maintained by the platform. Always " +
					"`false` for records created here; an imported platform record cannot be changed " +
					"or deleted.",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dnsRecordResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// stateFrom maps the API record set onto the model.
//
// `name` and `type` are NOT taken from the response: the API always answers
// with the fully qualified name, while the configuration is free to use the
// relative form. Overwriting a Required attribute with a value that differs
// from the configuration is exactly what raises "inconsistent result after
// apply" (CLAUDE.md pitfall #1) — so the configured name stays in `name`, and
// the canonical one is reported in `fqdn`.
func stateFrom(ctx context.Context, rs *client.DNSRecordSet, name, recordType types.String) (dnsRecordModel, diag.Diagnostics) {
	var diags diag.Diagnostics
	records, d := types.SetValueFrom(ctx, types.StringType, rs.Records)
	diags.Append(d...)

	m := dnsRecordModel{
		ID:              types.StringValue(rs.ID),
		ZoneID:          types.StringValue(rs.ZoneID),
		Name:            name,
		Type:            recordType,
		TTL:             types.Int64Value(rs.TTL),
		Records:         records,
		FQDN:            types.StringValue(rs.Name),
		IsSystemManaged: types.BoolValue(rs.IsSystemManaged),
		CreatedAt:       types.StringValue(rs.CreatedAt),
	}
	// Import path: nothing was configured yet, so the canonical name is the
	// best answer available.
	if m.Name.IsNull() || m.Name.IsUnknown() {
		m.Name = types.StringValue(rs.Name)
	}
	if m.Type.IsNull() || m.Type.IsUnknown() {
		m.Type = types.StringValue(rs.RecordType)
	}
	return m, diags
}

func valuesOf(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out
}

func (r *dnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values := valuesOf(ctx, plan.Records, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateDNSRecordSet(ctx, plan.ZoneID.ValueString(), client.DNSRecordSetCreateRequest{
		Name:       plan.Name.ValueString(),
		RecordType: plan.Type.ValueString(),
		TTL:        plan.TTL.ValueInt64(),
		Records:    values,
	})
	if err != nil {
		addWriteError(&resp.Diagnostics, "Failed to create DNS record", err)
		return
	}

	state, diags := stateFrom(ctx, created, plan.Name, plan.Type)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rs, err := r.client.GetDNSRecordSet(ctx, state.ZoneID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read DNS record", err.Error())
		return
	}
	next, diags := stateFrom(ctx, rs, state.Name, state.Type)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

// Update sends the value set whole — that is the API's model, and trying to
// compute a delta would be both wrong and racy.
func (r *dnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	values := valuesOf(ctx, plan.Records, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ttl := plan.TTL.ValueInt64()

	updated, err := r.client.UpdateDNSRecordSet(ctx, state.ZoneID.ValueString(), state.ID.ValueString(),
		client.DNSRecordSetUpdateRequest{TTL: &ttl, Records: values})
	if err != nil {
		addWriteError(&resp.Diagnostics, "Failed to update DNS record", err)
		return
	}

	next, diags := stateFrom(ctx, updated, plan.Name, plan.Type)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *dnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteDNSRecordSet(ctx, state.ZoneID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		addWriteError(&resp.Diagnostics, "Failed to delete DNS record", err)
	}
}

// addWriteError relays the API message as-is.
//
// Every refusal of this service is already written for the customer: the 409
// names the record in conflict or says the zone is not answered yet, and the
// 422 gives the shape expected for the type. Guessing which one it is here
// would attach the wrong explanation to the others.
func addWriteError(diags *diag.Diagnostics, summary string, err error) {
	diags.AddError(summary, err.Error())
}

// qualify turns the configured name into the fully qualified form the API
// stores, so an import can find the record by its identity.
func qualify(name, zoneName string) string {
	n := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	z := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zoneName), "."))
	if n == "" || n == "@" {
		return z
	}
	if n == z || strings.HasSuffix(n, "."+z) {
		return n
	}
	return n + "." + z
}

// ImportState takes `<zone_id>/<name>/<type>` — the identity of the record,
// since its UUID is not something a user has at hand. `name` accepts the same
// forms as the attribute.
func (r *dnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Expected `<zone_id>/<name>/<type>`, for example "+
				"`3f1c…/www/A` (use `@` for the zone itself).",
		)
		return
	}
	zoneID, name, recordType := parts[0], parts[1], strings.ToUpper(parts[2])

	zone, err := r.client.GetDNSZone(ctx, zoneID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the DNS zone", err.Error())
		return
	}
	rs, err := r.client.FindDNSRecordSet(ctx, zoneID, qualify(name, zone.Name), recordType)
	if err != nil {
		resp.Diagnostics.AddError("DNS record not found", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), rs.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), recordType)...)
}
