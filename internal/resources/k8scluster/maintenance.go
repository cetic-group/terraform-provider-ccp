package k8scluster

// `maintenance` — automatic update settings of the cluster
// (`GET/PUT /v1/k8s/clusters/{id}/maintenance`, cetic-cloud-platform#2143).
//
// A nested attribute of the cluster rather than a resource of its own: the
// settings live and die with the cluster, the API has no DELETE for them (a
// separate resource would have no meaningful destroy), and every cluster has
// them (platform defaults until changed). Optional+Computed: without the
// attribute the provider reads the settings and leaves them alone (console
// changes included); with it, only the fields written in the configuration
// are sent (the PUT is partial: an absent field keeps its value), and only
// when they differ from the state.
//
// Every nested attribute is Computed (the required ones are enforced by
// validators instead): an Optional nested attribute whose prior value holds a
// non-Computed child is taken by Terraform for a configured one, and a
// configuration without `maintenance` would then plan its removal on every
// run.

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	fenetreAttrTypes = map[string]attr.Type{
		"jours":        types.ListType{ElemType: types.Int64Type},
		"heure_debut":  types.StringType,
		"duree_heures": types.Int64Type,
		"fuseau":       types.StringType,
	}
	gelAttrTypes = map[string]attr.Type{
		"debut": types.StringType,
		"fin":   types.StringType,
	}
	maintenanceAttrTypes = map[string]attr.Type{
		"auto_patch":   types.BoolType,
		"auto_mineure": types.BoolType,
		"auto_majeure": types.BoolType,
		"fenetre":      types.ObjectType{AttrTypes: fenetreAttrTypes},
		"dates_gel":    types.ListType{ElemType: types.ObjectType{AttrTypes: gelAttrTypes}},
		"personnalise": types.BoolType,
	}
)

type maintenanceModel struct {
	AutoPatch    types.Bool   `tfsdk:"auto_patch"`
	AutoMineure  types.Bool   `tfsdk:"auto_mineure"`
	AutoMajeure  types.Bool   `tfsdk:"auto_majeure"`
	Fenetre      types.Object `tfsdk:"fenetre"`
	DatesGel     types.List   `tfsdk:"dates_gel"`
	Personnalise types.Bool   `tfsdk:"personnalise"`
}

type fenetreModel struct {
	Jours       types.List   `tfsdk:"jours"`
	HeureDebut  types.String `tfsdk:"heure_debut"`
	DureeHeures types.Int64  `tfsdk:"duree_heures"`
	Fuseau      types.String `tfsdk:"fuseau"`
}

type gelModel struct {
	Debut types.String `tfsdk:"debut"`
	Fin   types.String `tfsdk:"fin"`
}

var dateRegex = `^\d{4}-\d{2}-\d{2}$`

func maintenanceSchema() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Automatic Kubernetes updates of the cluster and their maintenance window. " +
			"Optional: without it the platform settings (defaults: patches on, minor and major off, " +
			"Tuesday to Thursday 02:00 for 4 h, Europe/Paris) are read back and left untouched. " +
			"Only the fields written are managed; removing a field or the whole attribute keeps the " +
			"current value. A patch is applied 30 days after its release even with `auto_patch = false`.",
		Optional:      true,
		Computed:      true,
		PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
		Attributes: map[string]schema.Attribute{
			"auto_patch": schema.BoolAttribute{
				MarkdownDescription: "Apply new patches of the current minor automatically, inside the window.",
				Optional:            true, Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auto_mineure": schema.BoolAttribute{
				MarkdownDescription: "Move to the next minor version automatically, inside the window.",
				Optional:            true, Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auto_majeure": schema.BoolAttribute{
				MarkdownDescription: "Move to the next major version automatically (no effect while no 2.x exists).",
				Optional:            true, Computed: true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"fenetre": schema.SingleNestedAttribute{
				MarkdownDescription: "Maintenance window: an automatic update only starts inside it. A whole block.",
				Optional:            true, Computed: true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"jours": schema.ListAttribute{
						MarkdownDescription: "ISO week days, 1 (Monday) to 7 (Sunday).",
						ElementType:         types.Int64Type,
						Optional:            true, Computed: true,
						Validators: []validator.List{
							listvalidator.IsRequired(),
							listvalidator.SizeBetween(1, 7),
							listvalidator.ValueInt64sAre(int64validator.Between(1, 7)),
						},
					},
					"heure_debut": schema.StringAttribute{
						MarkdownDescription: "Local start time, `HH:MM`.",
						Optional:            true, Computed: true,
						Validators: []validator.String{required(), stringvalidator.RegexMatches(
							mustRegexp(`^([01]\d|2[0-3]):[0-5]\d$`), "must be HH:MM")},
					},
					"duree_heures": schema.Int64Attribute{
						MarkdownDescription: "Duration in hours, 4 to 24.",
						Optional:            true, Computed: true,
						Validators: []validator.Int64{int64validator.Between(4, 24), int64RequiredIn{}},
					},
					"fuseau": schema.StringAttribute{
						MarkdownDescription: "IANA time zone (e.g. `Europe/Paris`).",
						Optional:            true, Computed: true,
						Validators: []validator.String{required(), stringvalidator.LengthBetween(1, 64)},
					},
				},
			},
			"dates_gel": schema.ListNestedAttribute{
				MarkdownDescription: "Freeze periods (at most 12, 31 days each, bounds included, `YYYY-MM-DD` " +
					"in the window's time zone): no window starting on those days is used. Replaces the " +
					"whole list; `[]` empties it.",
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				Validators:    []validator.List{listvalidator.SizeAtMost(12)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"debut": schema.StringAttribute{
							MarkdownDescription: "First day, `YYYY-MM-DD`.",
							Optional:            true, Computed: true,
							Validators: []validator.String{required(),
								stringvalidator.RegexMatches(mustRegexp(dateRegex), "must be YYYY-MM-DD")},
						},
						"fin": schema.StringAttribute{
							MarkdownDescription: "Last day (included), `YYYY-MM-DD`.",
							Optional:            true, Computed: true,
							Validators: []validator.String{required(),
								stringvalidator.RegexMatches(mustRegexp(dateRegex), "must be YYYY-MM-DD")},
						},
					},
				},
			},
			"personnalise": schema.BoolAttribute{
				MarkdownDescription: "`false` while the cluster follows the platform defaults (never configured).",
				Computed:            true,
			},
		},
	}
}

func known(v attr.Value) bool { return v != nil && !v.IsNull() && !v.IsUnknown() }

// maintenanceRequest builds the partial PUT from the CONFIGURED fields that
// differ from the prior state. send is false when there is nothing to send.
func maintenanceRequest(ctx context.Context, cfg, prior types.Object) (req client.K8sMaintenanceUpdateRequest, send bool, diags diag.Diagnostics) {
	if !known(cfg) {
		return req, false, nil
	}
	var c maintenanceModel
	diags.Append(cfg.As(ctx, &c, basetypes.ObjectAsOptions{UnhandledNullAsEmpty: true, UnhandledUnknownAsEmpty: true})...)
	var p maintenanceModel
	hasPrior := known(prior)
	if hasPrior {
		diags.Append(prior.As(ctx, &p, basetypes.ObjectAsOptions{UnhandledNullAsEmpty: true, UnhandledUnknownAsEmpty: true})...)
	}
	if diags.HasError() {
		return req, false, diags
	}
	boolField := func(cv, pv types.Bool) *bool {
		if !known(cv) || (hasPrior && cv.Equal(pv)) {
			return nil
		}
		v := cv.ValueBool()
		send = true
		return &v
	}
	req.AutoPatch = boolField(c.AutoPatch, p.AutoPatch)
	req.AutoMineure = boolField(c.AutoMineure, p.AutoMineure)
	req.AutoMajeure = boolField(c.AutoMajeure, p.AutoMajeure)
	if known(c.Fenetre) && !(hasPrior && fenetreEqual(ctx, c.Fenetre, p.Fenetre)) {
		f, d := fenetreFromObject(ctx, c.Fenetre)
		diags.Append(d...)
		req.Fenetre = &f
		send = true
	}
	if known(c.DatesGel) && !(hasPrior && gelsEqual(ctx, c.DatesGel, p.DatesGel)) {
		g, d := gelsFromList(ctx, c.DatesGel)
		diags.Append(d...)
		req.DatesGel = &g
		send = true
	}
	return req, send, diags
}

func fenetreFromObject(ctx context.Context, o types.Object) (client.K8sMaintenanceFenetre, diag.Diagnostics) {
	var f fenetreModel
	diags := o.As(ctx, &f, basetypes.ObjectAsOptions{})
	out := client.K8sMaintenanceFenetre{
		HeureDebut:  f.HeureDebut.ValueString(),
		DureeHeures: int(f.DureeHeures.ValueInt64()),
		Fuseau:      f.Fuseau.ValueString(),
	}
	var jours []int64
	diags.Append(f.Jours.ElementsAs(ctx, &jours, false)...)
	for _, j := range jours {
		out.Jours = append(out.Jours, int(j))
	}
	return out, diags
}

func gelsFromList(ctx context.Context, l types.List) ([]client.K8sMaintenanceGel, diag.Diagnostics) {
	var gels []gelModel
	diags := l.ElementsAs(ctx, &gels, false)
	out := make([]client.K8sMaintenanceGel, 0, len(gels))
	for _, g := range gels {
		out = append(out, client.K8sMaintenanceGel{Debut: g.Debut.ValueString(), Fin: g.Fin.ValueString()})
	}
	return out, diags
}

// canonical forms, as the API stores them: days sorted without duplicates,
// freeze periods sorted by start.
func canonicalJours(j []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, d := range j {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Ints(out)
	return out
}

func canonicalGels(g []client.K8sMaintenanceGel) []client.K8sMaintenanceGel {
	out := append([]client.K8sMaintenanceGel(nil), g...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Debut < out[j].Debut })
	return out
}

func fenetreSame(a, b client.K8sMaintenanceFenetre) bool {
	if a.HeureDebut != b.HeureDebut || a.DureeHeures != b.DureeHeures || a.Fuseau != b.Fuseau {
		return false
	}
	ja, jb := canonicalJours(a.Jours), canonicalJours(b.Jours)
	return fmt.Sprint(ja) == fmt.Sprint(jb)
}

func gelsSame(a, b []client.K8sMaintenanceGel) bool {
	return fmt.Sprint(canonicalGels(a)) == fmt.Sprint(canonicalGels(b))
}

func fenetreEqual(ctx context.Context, a, b types.Object) bool {
	if !known(a) || !known(b) {
		return false
	}
	fa, d1 := fenetreFromObject(ctx, a)
	fb, d2 := fenetreFromObject(ctx, b)
	return !d1.HasError() && !d2.HasError() && fenetreSame(fa, fb)
}

func gelsEqual(ctx context.Context, a, b types.List) bool {
	if !known(a) || !known(b) {
		return false
	}
	ga, d1 := gelsFromList(ctx, a)
	gb, d2 := gelsFromList(ctx, b)
	return !d1.HasError() && !d2.HasError() && gelsSame(ga, gb)
}

// maintenanceValue is the state value of the API settings. The window and
// the freeze periods keep the prior spelling (days or periods in another
// order) when they are the same once canonical: no diff, no inconsistent
// result after apply.
func maintenanceValue(ctx context.Context, api *client.K8sMaintenance, prior types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	var p maintenanceModel
	hasPrior := known(prior)
	if hasPrior {
		diags.Append(prior.As(ctx, &p, basetypes.ObjectAsOptions{UnhandledNullAsEmpty: true, UnhandledUnknownAsEmpty: true})...)
	}

	var fenetre types.Object
	if hasPrior && known(p.Fenetre) {
		if pf, d := fenetreFromObject(ctx, p.Fenetre); !d.HasError() && fenetreSame(pf, api.Fenetre) {
			fenetre = p.Fenetre
		}
	}
	if fenetre.IsNull() {
		jours := make([]attr.Value, 0, len(api.Fenetre.Jours))
		for _, j := range api.Fenetre.Jours {
			jours = append(jours, types.Int64Value(int64(j)))
		}
		jl, d := types.ListValue(types.Int64Type, jours)
		diags.Append(d...)
		f, d := types.ObjectValue(fenetreAttrTypes, map[string]attr.Value{
			"jours":        jl,
			"heure_debut":  types.StringValue(api.Fenetre.HeureDebut),
			"duree_heures": types.Int64Value(int64(api.Fenetre.DureeHeures)),
			"fuseau":       types.StringValue(api.Fenetre.Fuseau),
		})
		diags.Append(d...)
		fenetre = f
	}

	var gels types.List
	if hasPrior && known(p.DatesGel) {
		if pg, d := gelsFromList(ctx, p.DatesGel); !d.HasError() && gelsSame(pg, api.DatesGel) {
			gels = p.DatesGel
		}
	}
	if gels.IsNull() {
		elemType := types.ObjectType{AttrTypes: gelAttrTypes}
		elems := make([]attr.Value, 0, len(api.DatesGel))
		for _, g := range api.DatesGel {
			o, d := types.ObjectValue(gelAttrTypes, map[string]attr.Value{
				"debut": types.StringValue(g.Debut),
				"fin":   types.StringValue(g.Fin),
			})
			diags.Append(d...)
			elems = append(elems, o)
		}
		l, d := types.ListValue(elemType, elems)
		diags.Append(d...)
		gels = l
	}

	out, d := types.ObjectValue(maintenanceAttrTypes, map[string]attr.Value{
		"auto_patch":   types.BoolValue(api.AutoPatch),
		"auto_mineure": types.BoolValue(api.AutoMineure),
		"auto_majeure": types.BoolValue(api.AutoMajeure),
		"fenetre":      fenetre,
		"dates_gel":    gels,
		"personnalise": types.BoolValue(api.Personnalise),
	})
	diags.Append(d...)
	return out, diags
}

func mustRegexp(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// required makes a Computed nested attribute mandatory in the configuration
// once its parent is written (see the note at the top of this file).
func required() validator.String { return stringRequiredIn{} }

type stringRequiredIn struct{}

func (stringRequiredIn) Description(context.Context) string { return "required when its block is set" }
func (v stringRequiredIn) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (stringRequiredIn) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() {
		resp.Diagnostics.AddAttributeError(req.Path, "Missing required argument",
			fmt.Sprintf("%s is required when its block is set.", req.Path))
	}
}

type int64RequiredIn struct{}

func (int64RequiredIn) Description(context.Context) string { return "required when its block is set" }
func (v int64RequiredIn) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (int64RequiredIn) ValidateInt64(_ context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() {
		resp.Diagnostics.AddAttributeError(req.Path, "Missing required argument",
			fmt.Sprintf("%s is required when its block is set.", req.Path))
	}
}
