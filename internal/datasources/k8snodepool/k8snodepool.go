// Package k8snodepool implements the ccp_k8s_node_pool data source.
package k8snodepool

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*npDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*npDataSource)(nil)
)

func New() datasource.DataSource { return &npDataSource{} }

type npDataSource struct{ client *client.Client }

type taintModel struct {
	Key    types.String `tfsdk:"key"`
	Value  types.String `tfsdk:"value"`
	Effect types.String `tfsdk:"effect"`
}

type npDSModel struct {
	ID                    types.String `tfsdk:"id"`
	ClusterID             types.String `tfsdk:"cluster_id"`
	Name                  types.String `tfsdk:"name"`
	Plan                  types.String `tfsdk:"plan"`
	Replicas              types.Int64  `tfsdk:"replicas"`
	Labels                types.Map    `tfsdk:"labels"`
	Taints                types.List   `tfsdk:"taints"`
	MinSize               types.Int64  `tfsdk:"min_size"`
	MaxSize               types.Int64  `tfsdk:"max_size"`
	MachineDeploymentName types.String `tfsdk:"machine_deployment_name"`
	Status                types.String `tfsdk:"status"`
	ErrorMessage          types.String `tfsdk:"error_message"`
	CreatedAt             types.String `tfsdk:"created_at"`
	UpdatedAt             types.String `tfsdk:"updated_at"`
}

func (d *npDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_k8s_node_pool"
}

func (d *npDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a Kubernetes node pool by `(id, cluster_id)` or `(name, cluster_id)`.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Optional: true, Computed: true},
			"cluster_id": schema.StringAttribute{Required: true},
			"name":       schema.StringAttribute{Optional: true, Computed: true},
			"plan":       schema.StringAttribute{Computed: true},
			"replicas":   schema.Int64Attribute{Computed: true},
			"labels":     schema.MapAttribute{ElementType: types.StringType, Computed: true},
			"taints": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key":    schema.StringAttribute{Computed: true},
						"value":  schema.StringAttribute{Computed: true},
						"effect": schema.StringAttribute{Computed: true},
					},
				},
			},
			"min_size":                schema.Int64Attribute{Computed: true},
			"max_size":                schema.Int64Attribute{Computed: true},
			"machine_deployment_name": schema.StringAttribute{Computed: true},
			"status":                  schema.StringAttribute{Computed: true},
			"error_message":           schema.StringAttribute{Computed: true},
			"created_at":              schema.StringAttribute{Computed: true},
			"updated_at":              schema.StringAttribute{Computed: true},
		},
	}
}

func (d *npDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *npDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg npDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clusterID := cfg.ClusterID.ValueString()
	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Conflicting lookup arguments", "Provide either `id` or `name` (with `cluster_id`), not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing lookup arguments", "Provide either `id` or `name` (with `cluster_id`).")
		return
	}

	var found *client.K8sNodePool
	if hasID {
		got, err := d.client.GetK8sNodePool(ctx, clusterID, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read node pool", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListK8sNodePools(ctx, clusterID)
		if err != nil {
			resp.Diagnostics.AddError("Failed to list node pools", err.Error())
			return
		}
		wantName := cfg.Name.ValueString()
		matches := make([]int, 0, 1)
		for i := range list {
			if list[i].Name == wantName {
				matches = append(matches, i)
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError("Node pool not found", fmt.Sprintf("No pool named %q in cluster %q.", wantName, clusterID))
			return
		case 1:
			m := list[matches[0]]
			found = &m
		default:
			resp.Diagnostics.AddError("Multiple node pools matched", fmt.Sprintf("Found %d pools named %q in cluster %q.", len(matches), wantName, clusterID))
			return
		}
	}

	state := npDSModel{
		ID:        types.StringValue(found.ID),
		ClusterID: types.StringValue(found.ClusterID),
		Name:      types.StringValue(found.Name),
		Plan:      types.StringValue(found.Plan),
		Replicas:  types.Int64Value(int64(found.Replicas)),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
		UpdatedAt: types.StringValue(found.UpdatedAt.Format(time.RFC3339)),
	}
	if found.MinSize != nil {
		state.MinSize = types.Int64Value(int64(*found.MinSize))
	} else {
		state.MinSize = types.Int64Null()
	}
	if found.MaxSize != nil {
		state.MaxSize = types.Int64Value(int64(*found.MaxSize))
	} else {
		state.MaxSize = types.Int64Null()
	}
	if found.MachineDeploymentName != nil {
		state.MachineDeploymentName = types.StringValue(*found.MachineDeploymentName)
	} else {
		state.MachineDeploymentName = types.StringNull()
	}
	if found.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*found.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}

	labels, _ := types.MapValueFrom(ctx, types.StringType, found.Labels)
	state.Labels = labels

	taintList := make([]taintModel, 0, len(found.Taints))
	for _, t := range found.Taints {
		tm := taintModel{
			Key:    types.StringValue(t.Key),
			Effect: types.StringValue(t.Effect),
		}
		if t.Value != nil {
			tm.Value = types.StringValue(*t.Value)
		} else {
			tm.Value = types.StringNull()
		}
		taintList = append(taintList, tm)
	}
	taintsValue, diags := types.ListValueFrom(ctx, taintObjType(), taintList)
	resp.Diagnostics.Append(diags...)
	state.Taints = taintsValue

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func taintObjType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"key":    types.StringType,
		"value":  types.StringType,
		"effect": types.StringType,
	}}
}
