// Package k8snodepool implements the ccp_k8s_node_pool Terraform resource.
//
// Pool de workers ADDITIONNEL d'un cluster CCKS (l'initial pool est créé via
// `ccp_k8s_cluster.initial_pool`). Replicas hot-mutable (sans replace).
package k8snodepool

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/ccks"
	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
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
	ID         types.String `tfsdk:"id"`
	ClusterID  types.String `tfsdk:"cluster_id"`
	Name       types.String `tfsdk:"name"`
	Plan       types.String `tfsdk:"plan"`
	Replicas   types.Int64  `tfsdk:"replicas"`
	K8sVersion types.String `tfsdk:"k8s_version"`
	// CurrentK8sVersion is the version the pool's nodes actually run (null:
	// the pool inherits the control-plane version).
	CurrentK8sVersion types.String `tfsdk:"current_k8s_version"`
	DiskGB            types.Int64  `tfsdk:"disk_gb"`
	Timeouts          types.Object `tfsdk:"timeouts"`
	MinSize           types.Int64  `tfsdk:"min_size"`
	MaxSize           types.Int64  `tfsdk:"max_size"`
	Labels            types.Map    `tfsdk:"labels"`
	Taints            types.Set    `tfsdk:"taints"`
	Status            types.String `tfsdk:"status"`
	CreatedAt         types.String `tfsdk:"created_at"`
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
			"k8s_version": schema.StringAttribute{
				// Desired Kubernetes version of the worker nodes (#2143).
				// Optional+Computed: since #2143 the platform pins a pool that
				// inherited the control-plane version on an explicit version when
				// the control plane is upgraded, and patches pools automatically
				// in the maintenance window. Read-back follows ccks.Floor: the
				// configured value stays in the state while the pool runs it or a
				// higher version, so neither of those shows a diff.
				Optional: true,
				Computed: true,
				Validators: []validator.String{stringvalidator.RegexMatches(
					regexp.MustCompile(ccks.VersionRegex), "must be a version X.Y.Z (e.g. v1.34.8)")},
				MarkdownDescription: "Desired Kubernetes version of the worker nodes in this pool " +
					"(`vX.Y.Z`). Omit to keep the version the platform gives the pool (the control-plane " +
					"version at creation). Raising it upgrades the pool in place through the platform's " +
					"pool upgrade (one node at a time, PodDisruptionBudgets respected) and waits for the " +
					"end: the target must be `<=` the control-plane version and of the same minor. " +
					"A value below the version the pool runs (automatic patch, console upgrade) is " +
					"kept as is: the provider never downgrades a pool. The actual version is " +
					"`current_k8s_version`. A failed upgrade is relaunched by the next apply.",
			},
			"current_k8s_version": schema.StringAttribute{
				MarkdownDescription: "Kubernetes version the pool's nodes actually run: `k8s_version_effective` of " +
					"the platform, updated at the end of an upgrade; on a platform that does not report it yet, the " +
					"version last requested (the target during or after a failed upgrade, `status = \"error\"`). " +
					"`null` when the pool inherits the control-plane version.",
				Computed: true,
			},
			"timeouts": ccks.TimeoutsAttribute(map[string]string{
				"update": "Each wait of a version upgrade: the other upgrades of the cluster before starting " +
					"(default `30m`), then the upgrade of the pool (default `90m`).",
			}),
			"disk_gb": schema.Int64Attribute{
				// Root disk size (GB) of every node in the pool. No resize
				// endpoint exists for node pools — changing it forces
				// replacement of the pool (unlike replicas/k8s_version).
				Optional: true,
				Computed: true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				// UseStateForUnknown FIRST: an unset disk_gb is unknown in the
				// plan of any other change (Computed), and RequiresReplace
				// would then replace the pool for a replicas change.
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplace(),
				},
				MarkdownDescription: "Root disk size (GB) of every node in this pool. " +
					"Optional — when omitted the platform picks 20 GB (raised to the plan's minimum " +
					"disk if higher) and the value is read back without a diff. " +
					"Forces replacement of the pool on change (no resize endpoint).",
			},
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
	// k8s_version is Optional+Computed: the API returns the worker version, or
	// null when the pool inherits the control-plane version. Mirror that (null =
	// inherits). Matches the existing nullable-string pattern in the client.
	// k8s_version is the DESIRED version (ccks.Floor): the prior value stays
	// while the pool runs it or higher; current_k8s_version is the actual one.
	// Running version: k8s_version_effective (#2146) when reported, else the
	// pool's version (the target during or after a failed upgrade).
	running := p.K8sVersion
	if p.K8sVersionEffective != nil && *p.K8sVersionEffective != "" {
		running = p.K8sVersionEffective
	}
	m.K8sVersion = ccks.Floor(m.K8sVersion, running)
	if running != nil {
		m.CurrentK8sVersion = types.StringValue(*running)
	} else {
		m.CurrentK8sVersion = types.StringNull()
	}
	// disk_gb : contrairement à k8s_version, on ne sait pas si l'API l'échote
	// toujours dans sa réponse → on préfère la valeur reçue, sinon on
	// conserve le plan/état déjà présent dans `m` (jamais Unknown en state).
	switch {
	case p.DiskGB != nil:
		m.DiskGB = types.Int64Value(int64(*p.DiskGB))
	case m.DiskGB.IsUnknown():
		m.DiskGB = types.Int64Null()
	}
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
	if !plan.K8sVersion.IsNull() && !plan.K8sVersion.IsUnknown() {
		v := plan.K8sVersion.ValueString()
		createReq.K8sVersion = &v
	}
	if !plan.DiskGB.IsNull() && !plan.DiskGB.IsUnknown() {
		v := int(plan.DiskGB.ValueInt64())
		createReq.DiskGB = &v
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

// Default waits of a pool upgrade: the other upgrades of the cluster before
// starting, then the rolling (one node at a time, drains respecting the
// PodDisruptionBudgets). `timeouts.update` overrides both.
const (
	defaultSettle      = 30 * time.Minute
	defaultPoolUpgrade = 90 * time.Minute
)

func (r *poolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	clusterID, poolID := state.ClusterID.ValueString(), state.ID.ValueString()
	var upd client.K8sNodePoolUpdateRequest
	patch := false
	if !plan.Replicas.Equal(state.Replicas) {
		v := int(plan.Replicas.ValueInt64())
		upd.Replicas = &v
		patch = true
	}
	if !plan.MinSize.Equal(state.MinSize) {
		patch = true
		if plan.MinSize.IsNull() {
			z := 0
			upd.MinSize = &z
		} else {
			v := int(plan.MinSize.ValueInt64())
			upd.MinSize = &v
		}
	}
	if !plan.MaxSize.Equal(state.MaxSize) {
		patch = true
		if plan.MaxSize.IsNull() {
			z := 0
			upd.MaxSize = &z
		} else {
			v := int(plan.MaxSize.ValueInt64())
			upd.MaxSize = &v
		}
	}
	if !plan.Labels.Equal(state.Labels) && !plan.Labels.IsUnknown() {
		patch = true
		labels := map[string]string{}
		if !plan.Labels.IsNull() {
			plan.Labels.ElementsAs(ctx, &labels, false)
		}
		upd.Labels = labels
	}
	if !plan.Taints.Equal(state.Taints) && !plan.Taints.IsUnknown() {
		patch = true
		taints, err := setToTaints(ctx, plan.Taints)
		if err != nil {
			resp.Diagnostics.AddError("Failed to encode taints", err.Error())
			return
		}
		if taints == nil {
			taints = []client.NodePoolTaint{}
		}
		upd.Taints = taints
	}

	var final *client.K8sNodePool
	if patch {
		// k8s_version is never sent by PATCH any more: a version change goes
		// through the pool upgrade route below (#2143).
		updated, err := r.client.UpdateK8sNodePool(ctx, clusterID, poolID, upd)
		if err != nil {
			resp.Diagnostics.AddError("Failed to update node pool", err.Error())
			return
		}
		final = updated
	}

	// ── Kubernetes version of the pool (#2143) ────────────────────────────
	// ccks.EnsurePool waits for the other upgrades of the cluster first, then
	// compares, relaunches a failed upgrade towards the same version, and
	// never downgrades.
	if !plan.K8sVersion.IsNull() && !plan.K8sVersion.IsUnknown() && !plan.K8sVersion.Equal(state.K8sVersion) {
		// Persist what the PATCH did before the long wait: an upgrade that
		// fails or times out leaves a state matching the platform.
		if final != nil {
			partial := plan
			partial.K8sVersion = state.K8sVersion
			setState(ctx, &partial, final)
			resp.Diagnostics.Append(resp.State.Set(ctx, &partial)...)
		}
		upgraded, res, err := ccks.EnsurePool(ctx, r.client, clusterID, poolID, plan.K8sVersion.ValueString(),
			ccks.PoolTimeouts{
				Settle:  ccks.Timeout(plan.Timeouts, "update", defaultSettle),
				Upgrade: ccks.Timeout(plan.Timeouts, "update", defaultPoolUpgrade),
			})
		if err != nil {
			resp.Diagnostics.AddError("Failed to upgrade node pool", err.Error())
			return
		}
		if res.Warning != "" {
			resp.Diagnostics.AddWarning("Node pool not downgraded", res.Warning)
		}
		if res.Relaunched {
			resp.Diagnostics.AddWarning("Failed pool upgrade relaunched",
				fmt.Sprintf("The previous upgrade of the pool to %s had failed; it was relaunched and succeeded.",
					plan.K8sVersion.ValueString()))
		}
		final = upgraded
	}

	if final == nil {
		got, err := r.client.GetK8sNodePool(ctx, clusterID, poolID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read node pool", err.Error())
			return
		}
		final = got
	}
	setState(ctx, &plan, final)
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
