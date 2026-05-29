// Package k8snodepool implements the ccp_k8s_node_pool Terraform resource.
//
// Pool de workers ADDITIONNEL d'un cluster CCKS (l'initial pool est créé via
// `ccp_k8s_cluster.initial_pool`). Replicas hot-mutable (sans replace).
package k8snodepool

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*poolResource)(nil)
	_ resource.ResourceWithConfigure   = (*poolResource)(nil)
	_ resource.ResourceWithImportState = (*poolResource)(nil)
)

func New() resource.Resource { return &poolResource{} }

type poolResource struct{ client *client.Client }

type poolResourceModel struct {
	ID        types.String `tfsdk:"id"`
	ClusterID types.String `tfsdk:"cluster_id"`
	Name      types.String `tfsdk:"name"`
	Plan      types.String `tfsdk:"plan"`
	Replicas  types.Int64  `tfsdk:"replicas"`
	MinSize   types.Int64  `tfsdk:"min_size"`
	MaxSize   types.Int64  `tfsdk:"max_size"`
	Labels    types.Map    `tfsdk:"labels"`
	Taints    types.Set    `tfsdk:"taints"`
	Status    types.String `tfsdk:"status"`
	CreatedAt types.String `tfsdk:"created_at"`
}

// taintAttrTypes defines the attr.Type map for a single taint object within the Set.
var taintAttrTypes = map[string]attr.Type{
	"key":    types.StringType,
	"value":  types.StringType,
	"effect": types.StringType,
}

func (r *poolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_k8s_node_pool"
}

func (r *poolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Additional worker pool of a `ccp_k8s_cluster`. " +
			"`replicas` is hot-mutable (CAPI rolling update). Set `min_size` + `max_size` to enable " +
			"the cluster autoscaler on this pool.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cluster_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"plan": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"replicas": schema.Int64Attribute{Required: true},
			"min_size": schema.Int64Attribute{Optional: true},
			"max_size": schema.Int64Attribute{Optional: true},
			"labels": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"taints": schema.SetNestedAttribute{
				Optional: true,
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Taint key.",
						},
						"value": schema.StringAttribute{
							Optional:            true,
							MarkdownDescription: "Taint value (may be empty).",
						},
						"effect": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Taint effect. One of: `NoSchedule`, `PreferNoSchedule`, `NoExecute`.",
							Validators: []validator.String{
								stringvalidator.OneOf("NoSchedule", "PreferNoSchedule", "NoExecute"),
							},
						},
					},
				},
				MarkdownDescription: "Set of Kubernetes taints applied to all nodes in this pool.",
			},
			"status": schema.StringAttribute{
				// PAS de UseStateForUnknown : `status` est volatil (un scale/labels/
				// taints update le fait passer "active" → "updating" → "active").
				// Pinner la valeur d'état précédente ferait planifier "active" puis
				// l'apply retourne "updating" → "Provider produced inconsistent result".
				// On le laisse known-after-apply à chaque changement.
				Computed: true,
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *poolResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// taintsToSet converts an API []NodePoolTaint slice into a types.Set suitable for TF state.
func taintsToSet(ctx context.Context, apiTaints []NodePoolTaint) (types.Set, error) {
	elemType := types.ObjectType{AttrTypes: taintAttrTypes}
	if len(apiTaints) == 0 {
		return types.SetValueMust(elemType, []attr.Value{}), nil
	}
	elems := make([]attr.Value, 0, len(apiTaints))
	for _, t := range apiTaints {
		var valueAttr attr.Value
		if t.Value != nil {
			valueAttr = types.StringValue(*t.Value)
		} else {
			valueAttr = types.StringNull()
		}
		obj, diags := types.ObjectValue(taintAttrTypes, map[string]attr.Value{
			"key":    types.StringValue(t.Key),
			"value":  valueAttr,
			"effect": types.StringValue(t.Effect),
		})
		if diags.HasError() {
			return types.SetNull(elemType), fmt.Errorf("building taint object: %v", diags)
		}
		elems = append(elems, obj)
	}
	set, diags := types.SetValue(elemType, elems)
	if diags.HasError() {
		return types.SetNull(elemType), fmt.Errorf("building taints set: %v", diags)
	}
	return set, nil
}

// setToTaints converts a types.Set from TF state/plan into []NodePoolTaint for API calls.
func setToTaints(ctx context.Context, s types.Set) ([]NodePoolTaint, error) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	type taintModel struct {
		Key    types.String `tfsdk:"key"`
		Value  types.String `tfsdk:"value"`
		Effect types.String `tfsdk:"effect"`
	}
	var models []taintModel
	if diags := s.ElementsAs(ctx, &models, false); diags.HasError() {
		return nil, fmt.Errorf("decoding taints: %v", diags)
	}
	taints := make([]client.NodePoolTaint, 0, len(models))
	for _, m := range models {
		t := client.NodePoolTaint{
			Key:    m.Key.ValueString(),
			Effect: m.Effect.ValueString(),
		}
		if !m.Value.IsNull() && !m.Value.IsUnknown() {
			v := m.Value.ValueString()
			t.Value = &v
		}
		taints = append(taints, t)
	}
	return taints, nil
}

// NodePoolTaint is a local alias kept for package-internal clarity (re-exported from client).
type NodePoolTaint = client.NodePoolTaint

func setState(ctx context.Context, m *poolResourceModel, p *client.K8sNodePool) {
	m.ID = types.StringValue(p.ID)
	m.ClusterID = types.StringValue(p.ClusterID)
	m.Name = types.StringValue(p.Name)
	m.Plan = types.StringValue(p.Plan)
	m.Replicas = types.Int64Value(int64(p.Replicas))
	// L'autoscaler est DÉSACTIVÉ ⟺ max_size absent ou 0 (annotations min=0/max=0).
	// Le backend stocke 0/0 quand on désactive (un PATCH ne peut pas effacer un
	// champ → on envoie 0). Comme `min_size`/`max_size` sont Optional (non-Computed),
	// le state final doit == la config : on normalise donc l'état désactivé (0/0)
	// vers null/null, sinon "inconsistent result: was null, but now 0".
	// Quand l'autoscaler est activé (max_size > 0), on garde les valeurs réelles —
	// y compris min_size=0 (scale-to-zero légitime).
	if p.MaxSize != nil && *p.MaxSize > 0 {
		m.MaxSize = types.Int64Value(int64(*p.MaxSize))
		if p.MinSize != nil {
			m.MinSize = types.Int64Value(int64(*p.MinSize))
		} else {
			m.MinSize = types.Int64Null()
		}
	} else {
		m.MinSize = types.Int64Null()
		m.MaxSize = types.Int64Null()
	}
	m.Status = types.StringValue(p.Status)
	m.CreatedAt = types.StringValue(p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	labels, _ := types.MapValueFrom(ctx, types.StringType, p.Labels)
	m.Labels = labels
	taints, err := taintsToSet(ctx, p.Taints)
	if err != nil {
		// non-fatal: leave previous state intact
		return
	}
	m.Taints = taints
}

func (r *poolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.K8sNodePoolCreateRequest{
		Name:     plan.Name.ValueString(),
		Plan:     plan.Plan.ValueString(),
		Replicas: int(plan.Replicas.ValueInt64()),
	}
	if !plan.MinSize.IsNull() && !plan.MinSize.IsUnknown() {
		v := int(plan.MinSize.ValueInt64())
		createReq.MinSize = &v
	}
	if !plan.MaxSize.IsNull() && !plan.MaxSize.IsUnknown() {
		v := int(plan.MaxSize.ValueInt64())
		createReq.MaxSize = &v
	}
	if !plan.Labels.IsNull() && !plan.Labels.IsUnknown() {
		labels := map[string]string{}
		plan.Labels.ElementsAs(ctx, &labels, false)
		createReq.Labels = labels
	}
	if !plan.Taints.IsNull() && !plan.Taints.IsUnknown() {
		taints, err := setToTaints(ctx, plan.Taints)
		if err != nil {
			resp.Diagnostics.AddError("Failed to encode taints", err.Error())
			return
		}
		createReq.Taints = taints
	}
	created, err := r.client.CreateK8sNodePool(ctx, plan.ClusterID.ValueString(), createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create node pool", err.Error())
		return
	}
	setState(ctx, &plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *poolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetK8sNodePool(ctx, state.ClusterID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read node pool", err.Error())
		return
	}
	setState(ctx, &state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *poolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var upd client.K8sNodePoolUpdateRequest
	if !plan.Replicas.Equal(state.Replicas) {
		v := int(plan.Replicas.ValueInt64())
		upd.Replicas = &v
	}
	if !plan.MinSize.Equal(state.MinSize) {
		if plan.MinSize.IsNull() {
			z := 0
			upd.MinSize = &z
		} else {
			v := int(plan.MinSize.ValueInt64())
			upd.MinSize = &v
		}
	}
	if !plan.MaxSize.Equal(state.MaxSize) {
		if plan.MaxSize.IsNull() {
			z := 0
			upd.MaxSize = &z
		} else {
			v := int(plan.MaxSize.ValueInt64())
			upd.MaxSize = &v
		}
	}
	if !plan.Labels.Equal(state.Labels) {
		labels := map[string]string{}
		if !plan.Labels.IsNull() && !plan.Labels.IsUnknown() {
			plan.Labels.ElementsAs(ctx, &labels, false)
		}
		upd.Labels = labels
	}
	if !plan.Taints.Equal(state.Taints) {
		taints, err := setToTaints(ctx, plan.Taints)
		if err != nil {
			resp.Diagnostics.AddError("Failed to encode taints", err.Error())
			return
		}
		upd.Taints = taints
	}
	updated, err := r.client.UpdateK8sNodePool(ctx, state.ClusterID.ValueString(), state.ID.ValueString(), upd)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update node pool", err.Error())
		return
	}
	setState(ctx, &plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *poolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteK8sNodePool(ctx, state.ClusterID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete node pool", err.Error())
	}
}

// ImportState : "<cluster_id>/<pool_id>"
func (r *poolResource) ImportState(_ context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitID(req.ID)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Import ID format",
			"Expected `<cluster_id>/<pool_id>`, got "+req.ID)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(_ctx(req), path.Root("cluster_id"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(_ctx(req), path.Root("id"), parts[1])...)
}

func _ctx(_ resource.ImportStateRequest) context.Context { return context.Background() }
func splitID(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
