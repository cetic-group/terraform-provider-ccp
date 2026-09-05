// Package containerscaleset implements the ccp_container_scale_set data source.
package containerscaleset

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*cssDS)(nil)
	_ datasource.DataSourceWithConfigure = (*cssDS)(nil)
)

func New() datasource.DataSource { return &cssDS{} }

type cssDS struct{ client *client.Client }

type cssDSModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Region           types.String `tfsdk:"region"`
	Plan             types.String `tfsdk:"plan"`
	Template         types.String `tfsdk:"template"`
	VnetID           types.String `tfsdk:"vnet_id"`
	MinInstances     types.Int64  `tfsdk:"min_instances"`
	MaxInstances     types.Int64  `tfsdk:"max_instances"`
	DesiredInstances types.Int64  `tfsdk:"desired_instances"`
	AutoRepair       types.Bool   `tfsdk:"auto_repair"`
	Status           types.String `tfsdk:"status"`
	ErrorMessage     types.String `tfsdk:"error_message"`
	Tags             types.List   `tfsdk:"tags"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
	Containers       types.List   `tfsdk:"containers"`
}

func (d *cssDS) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_container_scale_set"
}

func (d *cssDS) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a container scale set by `id` or `(name, region)`.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Optional: true, Computed: true},
			"name":              schema.StringAttribute{Optional: true, Computed: true},
			"region":            schema.StringAttribute{Optional: true, Computed: true},
			"plan":              schema.StringAttribute{Computed: true},
			"template":          schema.StringAttribute{Computed: true},
			"vnet_id":           schema.StringAttribute{Computed: true},
			"min_instances":     schema.Int64Attribute{Computed: true},
			"max_instances":     schema.Int64Attribute{Computed: true},
			"desired_instances": schema.Int64Attribute{Computed: true},
			"auto_repair":       schema.BoolAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
			"error_message":     schema.StringAttribute{Computed: true},
			"tags":              schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":        schema.StringAttribute{Computed: true},
			"updated_at":        schema.StringAttribute{Computed: true},
			// Les membres du scale set, tels que la plateforme les connaît AU
			// MOMENT DE LA LECTURE. Ils permettent de placer un scale set
			// derrière une Application Gateway ou un load balancer, en
			// dépliant les membres avec `for_each` (#75).
			//
			// ⚠️ Cet ensemble DÉRIVE dès que l'effectif change : un membre
			// ajouté ou retiré par l'autoscaler n'est pas dans l'état
			// Terraform, et le `plan` suivant proposera d'ajuster les
			// backends. C'est utilisable sur un effectif fixe ; pour un
			// groupe qui scale réellement, il faut une cible `scale_set_id`
			// réconciliée par la plateforme — voir la note de la
			// documentation.
			"containers": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":         schema.StringAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"status":     schema.StringAttribute{Computed: true},
						"ip_address": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *cssDS) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *cssDS) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg cssDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	hasRegion := !cfg.Region.IsNull() && !cfg.Region.IsUnknown() && cfg.Region.ValueString() != ""

	switch {
	case hasID && (hasName || hasRegion):
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id`, or both `name` and `region`.")
		return
	case !hasID && !(hasName && hasRegion):
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id`, or both `name` and `region`.")
		return
	}

	var found *client.ContainerScaleSet
	if hasID {
		got, err := d.client.GetContainerScaleSet(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read container scale set", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListContainerScaleSets(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list container scale sets", err.Error())
			return
		}
		wantName, wantRegion := cfg.Name.ValueString(), cfg.Region.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == wantName && list[i].Region == wantRegion {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("Container scale set not found", fmt.Sprintf("No scale set named %q in region %q.", wantName, wantRegion))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple container scale sets matched", fmt.Sprintf("Found %d in region %q.", len(matches), wantRegion))
			return
		}
	}

	// ⚠️ **La liste ne porte pas les membres, seul le détail les porte.**
	// Une recherche par `(name, region)` passe par `List…`, dont la charge
	// utile n'a pas de `containers` : sans cette relecture, l'attribut serait
	// vide ou plein selon la FORME de la recherche, pour le même scale set.
	// Une incohérence silencieuse, et impossible à diagnostiquer depuis une
	// configuration (#75).
	if found.Containers == nil {
		if detail, err := d.client.GetContainerScaleSet(ctx, found.ID); err == nil {
			found = detail
		} else {
			resp.Diagnostics.AddWarning(
				"Scale set members unavailable",
				fmt.Sprintf("Could not read the members of container scale set %s: %s. "+
					"`containers` is empty; the rest of the data source is accurate.",
					found.ID, err.Error()),
			)
		}
	}

	state := cssDSModel{
		ID:               types.StringValue(found.ID),
		Name:             types.StringValue(found.Name),
		Region:           types.StringValue(found.Region),
		Plan:             types.StringValue(found.Plan),
		Template:         types.StringValue(found.Template),
		MinInstances:     types.Int64Value(int64(found.MinInstances)),
		MaxInstances:     types.Int64Value(int64(found.MaxInstances)),
		DesiredInstances: types.Int64Value(int64(found.DesiredInstances)),
		AutoRepair:       types.BoolValue(found.AutoRepair),
		Status:           types.StringValue(found.Status),
		CreatedAt:        types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt:        types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	if found.VnetID != nil {
		state.VnetID = types.StringValue(*found.VnetID)
	} else {
		state.VnetID = types.StringNull()
	}
	if found.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*found.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	membres := make([]membreModel, 0, len(found.Containers))
	for _, m := range found.Containers {
		ip := types.StringNull()
		if m.IPAddress != nil {
			ip = types.StringValue(*m.IPAddress)
		}
		membres = append(membres, membreModel{
			ID:        types.StringValue(m.ID),
			Name:      types.StringValue(m.Name),
			Status:    types.StringValue(m.Status),
			IPAddress: ip,
		})
	}
	liste, d2 := types.ListValueFrom(ctx, membreObjectType(), membres)
	resp.Diagnostics.Append(d2...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Containers = liste

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// membreModel est la projection d'un membre du scale set : juste ce qu'il faut
// pour l'inscrire comme cible d'une Application Gateway ou d'un load balancer.
type membreModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Status    types.String `tfsdk:"status"`
	IPAddress types.String `tfsdk:"ip_address"`
}

func membreObjectType() attr.Type {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id":         types.StringType,
		"name":       types.StringType,
		"status":     types.StringType,
		"ip_address": types.StringType,
	}}
}
