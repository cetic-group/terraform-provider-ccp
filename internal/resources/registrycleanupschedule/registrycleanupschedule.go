// Package registrycleanupschedule implements the ccp_registry_cleanup_schedule
// Terraform resource (cetic-cloud-platform#1984).
//
// A cleanup schedule selects tags of a CETIC Container Registry by repository
// patterns × tag patterns and a criterion, and runs on an optional cron. It
// never deletes directly: a run sends the matching images to the registry's
// recycle bin, purged after the grace period. Up to 4 schedules per registry.
//
// Simulations and immediate runs (`…/simulate`, `…/run`) are actions, not
// state: they are deliberately NOT modelled — applying a configuration never
// triggers a run.
//
// PATCH semantics on the platform: an absent field is "unchanged",
// `"cron": null` makes the schedule manual, `"valeur": null` clears the value,
// and changing `critere` without `valeur` is refused. The provider sends the
// WHOLE desired state on every write (see client.RegistryCleanupScheduleWrite),
// so no argument relies on "absent = keep": every argument is the user's
// value, and removing `cron` from the configuration really sends `null`.
package registrycleanupschedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*cleanupScheduleResource)(nil)
	_ resource.ResourceWithConfigure      = (*cleanupScheduleResource)(nil)
	_ resource.ResourceWithImportState    = (*cleanupScheduleResource)(nil)
	_ resource.ResourceWithValidateConfig = (*cleanupScheduleResource)(nil)
)

func New() resource.Resource { return &cleanupScheduleResource{} }

type cleanupScheduleResource struct{ client *client.Client }

type cleanupScheduleModel struct {
	ID               types.String `tfsdk:"id"`
	RegistryID       types.String `tfsdk:"registry_id"`
	Name             types.String `tfsdk:"name"`
	RepoPatterns     types.List   `tfsdk:"repo_patterns"`
	TagPatterns      types.List   `tfsdk:"tag_patterns"`
	Criterion        types.String `tfsdk:"criterion"`
	Value            types.Int64  `tfsdk:"value"`
	Cron             types.String `tfsdk:"cron"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	Slot             types.Int64  `tfsdk:"slot"`
	LastDispatchedAt types.String `tfsdk:"last_dispatched_at"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func (r *cleanupScheduleResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_registry_cleanup_schedule"
}

func (r *cleanupScheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Scheduled cleanup of the images of a `ccp_registry`: repository patterns × tag " +
			"patterns, a criterion and an optional UTC cron. A run never deletes directly — matching images " +
			"go to the registry's recycle bin and are purged after its grace period. Up to 4 schedules per " +
			"registry. Simulations and immediate runs are not managed by Terraform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the schedule.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"registry_id": schema.StringAttribute{
				MarkdownDescription: "UUID of the parent `ccp_registry` (which must be `active`). Forces replacement.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name, unique within the registry: 1 to 64 characters, starting with a letter " +
					"or a digit, then letters, digits, spaces and `. _ -`.",
				Required: true,
			},
			"repo_patterns": schema.ListAttribute{
				MarkdownDescription: "1 to 10 distinct repository patterns: lowercase letters, digits, `. _ - /` " +
					"and the wildcards `*` (any sequence, `/` included) and `?` (one character); no `**`; " +
					"255 characters at most.",
				ElementType: types.StringType,
				Required:    true,
			},
			"tag_patterns": schema.ListAttribute{
				MarkdownDescription: "1 to 10 distinct tag patterns: letters, digits, `. _ -` and the wildcards " +
					"`*` `?`; no `**`; 128 characters at most.",
				ElementType: types.StringType,
				Required:    true,
			},
			"criterion": schema.StringAttribute{
				MarkdownDescription: "`all` (every matching tag), `older_than_days` or `keep_last_n` (keep the N " +
					"most recent tags per repository and per pattern pair).",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(critereAll, critereOlderThan, critereKeepLastN),
				},
			},
			"value": schema.Int64Attribute{
				MarkdownDescription: "Days (1 to 3650) for `older_than_days`, number of tags kept (1 to 1000) " +
					"for `keep_last_n`. Must be omitted for `all`.",
				Optional: true,
			},
			"cron": schema.StringAttribute{
				MarkdownDescription: "5-field cron in UTC (`*`, `*/N`, values, ranges `a-b`, lists `a,b` without " +
					"`*`); day of month and day of week cannot both be restricted. Omitted (or empty): the " +
					"schedule only runs on demand — removing it from the configuration makes the schedule manual. " +
					"A scheduled run condemns images **without a prior simulation**.",
				Optional: true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "`false`: never runs (it can still be simulated). Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"slot": schema.Int64Attribute{
				MarkdownDescription: "Slot of the schedule in its registry (1 to 4), assigned at creation.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"last_dispatched_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 time of the last scheduled dispatch — or of the last creation or " +
					"update, which the scheduler never catches up on (a run due just before an update is skipped).",
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 creation timestamp.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the most recent update.",
				Computed:            true,
			},
		},
	}
}

func (r *cleanupScheduleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// listElems turns a configuration list into plan-time elements; the second
// result is true when the list itself is unknown (or null).
func listElems(l types.List) ([]patternElem, bool) {
	if l.IsNull() || l.IsUnknown() {
		return nil, true
	}
	var out []patternElem
	for _, v := range l.Elements() {
		s, _ := v.(types.String)
		out = append(out, patternElem{Value: s.ValueString(), Null: s.IsNull(), Unknown: s.IsUnknown()})
	}
	return out, false
}

// ValidateConfig mirrors the platform's write validation (see validate.go).
// Every check is gated on its inputs being known: an unknown value (module
// variable, another resource's attribute) is never judged.
func (r *cleanupScheduleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg cleanupScheduleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateModel(cfg)...)
}

func validateModel(cfg cleanupScheduleModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !cfg.Name.IsNull() && !cfg.Name.IsUnknown() {
		if msg := checkName(cfg.Name.ValueString()); msg != "" {
			diags.AddAttributeError(path.Root("name"), "Invalid cleanup schedule name", msg)
		}
	}
	repos, reposUnknown := listElems(cfg.RepoPatterns)
	if !reposUnknown {
		for _, msg := range checkPatterns(repoKind, repos) {
			diags.AddAttributeError(path.Root("repo_patterns"), "Invalid repository patterns", msg)
		}
	}
	tags, tagsUnknown := listElems(cfg.TagPatterns)
	if !tagsUnknown {
		for _, msg := range checkPatterns(tagKind, tags) {
			diags.AddAttributeError(path.Root("tag_patterns"), "Invalid tag patterns", msg)
		}
	}
	if !cfg.Cron.IsNull() && !cfg.Cron.IsUnknown() {
		if msg := checkCron(cfg.Cron.ValueString()); msg != "" {
			diags.AddAttributeError(path.Root("cron"), "Invalid cron", msg)
		}
	}
	if !cfg.Criterion.IsNull() && !cfg.Criterion.IsUnknown() {
		var value *int64
		if !cfg.Value.IsNull() && !cfg.Value.IsUnknown() {
			v := cfg.Value.ValueInt64()
			value = &v
		}
		everything := coversEverything(anyUniversal(repos, true, reposUnknown), anyUniversal(tags, false, tagsUnknown))
		if msg := checkCriterion(cfg.Criterion.ValueString(), value, cfg.Value.IsUnknown(), everything); msg != "" {
			diags.AddAttributeError(path.Root("value"), "Invalid cleanup criterion", msg)
		}
	}
	return diags
}

func stringsOf(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	out := []string{}
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out
}

// writeBody builds the full desired state sent on POST and PATCH.
func writeBody(ctx context.Context, m cleanupScheduleModel, diags *diag.Diagnostics) client.RegistryCleanupScheduleWrite {
	body := client.RegistryCleanupScheduleWrite{
		Name:         m.Name.ValueString(),
		RepoPatterns: stringsOf(ctx, m.RepoPatterns, diags),
		TagPatterns:  stringsOf(ctx, m.TagPatterns, diags),
		Critere:      m.Criterion.ValueString(),
		Enabled:      m.Enabled.IsNull() || m.Enabled.IsUnknown() || m.Enabled.ValueBool(),
	}
	if !m.Value.IsNull() && !m.Value.IsUnknown() {
		v := m.Value.ValueInt64()
		body.Valeur = &v
	}
	if !m.Cron.IsNull() && !m.Cron.IsUnknown() {
		c := m.Cron.ValueString()
		body.Cron = &c
	}
	return body
}

func timeString(t time.Time) types.String { return types.StringValue(t.Format(time.RFC3339)) }

// applyComputed copies the server-side attributes only. After a write the
// arguments keep the PLANNED values: the platform strips names and patterns
// and collapses cron whitespace, and writing that back over the plan would
// fail with "Provider produced inconsistent result after apply".
func applyComputed(m *cleanupScheduleModel, s *client.RegistryCleanupSchedule) {
	m.ID = types.StringValue(s.ID)
	m.Slot = types.Int64Value(s.Slot)
	if s.LastDispatchedAt != nil {
		m.LastDispatchedAt = timeString(*s.LastDispatchedAt)
	} else {
		m.LastDispatchedAt = types.StringNull()
	}
	m.CreatedAt = timeString(s.CreatedAt)
	m.UpdatedAt = timeString(s.UpdatedAt)
}

// keepPatterns keeps the prior spelling of a pattern list when the platform
// stores the same patterns once stripped; otherwise (real drift, import) it
// takes the platform's list.
func keepPatterns(prior types.List, api []string) types.List {
	if !prior.IsNull() && !prior.IsUnknown() {
		el := prior.Elements()
		if len(el) == len(api) {
			same := true
			for i, v := range el {
				s, ok := v.(types.String)
				if !ok || s.IsNull() || s.IsUnknown() || pyStrip(s.ValueString()) != api[i] {
					same = false
					break
				}
			}
			if same {
				return prior
			}
		}
	}
	vals := make([]attr.Value, 0, len(api))
	for _, p := range api {
		vals = append(vals, types.StringValue(p))
	}
	return types.ListValueMust(types.StringType, vals)
}

// applyRead maps a refreshed schedule onto the prior state. Arguments the
// platform normalizes keep the prior spelling when it means the same thing,
// so `name = " nightly"` or `cron = "0  3 * * *"` never produce a perpetual
// diff; any other difference is reported as drift.
func applyRead(m *cleanupScheduleModel, s *client.RegistryCleanupSchedule) {
	if m.Name.IsNull() || m.Name.IsUnknown() || pyStrip(m.Name.ValueString()) != s.Name {
		m.Name = types.StringValue(s.Name)
	}
	m.RepoPatterns = keepPatterns(m.RepoPatterns, s.RepoPatterns)
	m.TagPatterns = keepPatterns(m.TagPatterns, s.TagPatterns)
	m.Criterion = types.StringValue(s.Critere)
	if s.Valeur != nil {
		m.Value = types.Int64Value(*s.Valeur)
	} else {
		m.Value = types.Int64Null()
	}
	priorCron := (*string)(nil)
	if !m.Cron.IsNull() && !m.Cron.IsUnknown() {
		priorCron = normalizeCron(m.Cron.ValueString())
	}
	samecron := (priorCron == nil && s.Cron == nil) ||
		(priorCron != nil && s.Cron != nil && *priorCron == *s.Cron)
	if !samecron || m.Cron.IsUnknown() {
		if s.Cron == nil {
			m.Cron = types.StringNull()
		} else {
			m.Cron = types.StringValue(*s.Cron)
		}
	}
	m.Enabled = types.BoolValue(s.Enabled)
	applyComputed(m, s)
}

func (r *cleanupScheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cleanupScheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := writeBody(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateRegistryCleanupSchedule(ctx, plan.RegistryID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create registry cleanup schedule", err.Error())
		return
	}
	applyComputed(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// normalizedWrite is a write body as the platform stores it.
func normalizedWrite(w client.RegistryCleanupScheduleWrite) client.RegistryCleanupScheduleWrite {
	strip := func(l []string) []string {
		out := make([]string, 0, len(l))
		for _, p := range l {
			out = append(out, pyStrip(p))
		}
		return out
	}
	w.Name = pyStrip(w.Name)
	w.RepoPatterns, w.TagPatterns = strip(w.RepoPatterns), strip(w.TagPatterns)
	if w.Cron != nil {
		w.Cron = normalizeCron(*w.Cron)
	}
	return w
}

// sameWrite: would the platform store the same schedule from both bodies?
func sameWrite(a, b client.RegistryCleanupScheduleWrite) bool {
	a, b = normalizedWrite(a), normalizedWrite(b)
	eqList := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	eqPtr := func(x, y *int64) bool { return (x == nil && y == nil) || (x != nil && y != nil && *x == *y) }
	eqStr := func(x, y *string) bool { return (x == nil && y == nil) || (x != nil && y != nil && *x == *y) }
	return a.Name == b.Name && eqList(a.RepoPatterns, b.RepoPatterns) && eqList(a.TagPatterns, b.TagPatterns) &&
		a.Critere == b.Critere && eqPtr(a.Valeur, b.Valeur) && eqStr(a.Cron, b.Cron) && a.Enabled == b.Enabled
}

func (r *cleanupScheduleResource) find(ctx context.Context, registryID, id string) (*client.RegistryCleanupSchedule, error) {
	list, err := r.client.ListRegistryCleanupSchedules(ctx, registryID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].ID, id) {
			return &list[i], nil
		}
	}
	return nil, nil
}

func (r *cleanupScheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cleanupScheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.find(ctx, state.RegistryID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read registry cleanup schedules", err.Error())
		return
	}
	if got == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	applyRead(&state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *cleanupScheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state cleanupScheduleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := writeBody(ctx, plan, &resp.Diagnostics)
	prior := writeBody(ctx, state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// A PATCH resets the scheduler barrier (`last_dispatched_at`) even when it
	// changes nothing, which can skip a run that is due. A plan that only
	// respells a value the platform normalizes (`" nightly"` vs `nightly`,
	// `"0  3 * * *"`, no cron vs `""`) sends nothing: the state takes the plan,
	// with the server-side attributes unchanged.
	if sameWrite(body, prior) {
		plan.ID, plan.Slot, plan.CreatedAt = state.ID, state.Slot, state.CreatedAt
		plan.LastDispatchedAt, plan.UpdatedAt = state.LastDispatchedAt, state.UpdatedAt
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		return
	}
	updated, err := r.client.UpdateRegistryCleanupSchedule(ctx, state.RegistryID.ValueString(), state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update registry cleanup schedule", err.Error())
		return
	}
	applyComputed(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cleanupScheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cleanupScheduleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRegistryCleanupSchedule(ctx, state.RegistryID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete registry cleanup schedule", err.Error())
	}
}

// ImportState parses `<registry_id>/<schedule_id>` (same convention as
// ccp_registry_acl).
func (r *cleanupScheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			"Expected `<registry_id>/<schedule_id>`, got: "+req.ID)
		return
	}
	// The API renders UUIDs in lowercase: an ID typed in uppercase must still
	// find its schedule, and must not differ from `ccp_registry.id` in state.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("registry_id"), strings.ToLower(parts[0]))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.ToLower(parts[1]))...)
}
