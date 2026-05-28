// Package dbpginstance implements the ccp_db_pg_instance Terraform resource.
//
// Instance PostgreSQL managée (CloudNativePG sur le cluster K8s tenant).
// Provisioning asynchrone (5-10 min) — Create poll jusqu'à status=active.
// Pas d'Update (toutes les modifs forcent un replace en v1).
package dbpginstance

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*dbpgResource)(nil)
	_ resource.ResourceWithConfigure   = (*dbpgResource)(nil)
	_ resource.ResourceWithImportState = (*dbpgResource)(nil)
)

func New() resource.Resource { return &dbpgResource{} }

type dbpgResource struct{ client *client.Client }

type dbpgResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Region          types.String `tfsdk:"region"`
	VpcID           types.String `tfsdk:"vpc_id"`
	VnetID          types.String `tfsdk:"vnet_id"`
	Plan            types.String `tfsdk:"plan"`
	StorageGB       types.Int64  `tfsdk:"storage_gb"`
	Replicas        types.Int64  `tfsdk:"replicas"`
	Tier            types.String `tfsdk:"tier"`
	EngineVersion   types.String `tfsdk:"engine_version"`
	Status          types.String `tfsdk:"status"`
	EndpointVnetIP  types.String `tfsdk:"endpoint_vnet_ip"`
	EndpointPort    types.Int64  `tfsdk:"endpoint_port"`
	AdminUsername   types.String `tfsdk:"admin_username"`
	AdminDatabase   types.String `tfsdk:"admin_database"`
	CPUMillicores   types.Int64  `tfsdk:"cpu_millicores"`
	MemoryMB        types.Int64  `tfsdk:"memory_mb"`
	Tags            types.List   `tfsdk:"tags"`
}

func (r *dbpgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_db_pg_instance"
}

func (r *dbpgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "CETIC Cloud managed PostgreSQL instance (DBaaS, Phase 5). Backed by " +
			"CloudNativePG in the tenant's K8s cluster. Provisioning is async (~5-10 min). All " +
			"attributes force replacement — to scale, recreate the instance.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"region": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vpc_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"vnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"plan": schema.StringAttribute{
				MarkdownDescription: "nano | micro | small | medium | large | xlarge.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"storage_gb": schema.Int64Attribute{
				Required: true,
			},
			"replicas": schema.Int64Attribute{
				MarkdownDescription: "1 (dev) ou 3 (prod, HA). Forces replacement.",
				Optional:            true,
				Computed:            true,
			},
			"tier": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"engine_version": schema.StringAttribute{
				MarkdownDescription: "Major version (default '16'). Versions exposées sont gérées via le backoffice CETIC.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint_vnet_ip": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint_port": schema.Int64Attribute{
				Computed: true,
			},
			"admin_username": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"admin_database": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cpu_millicores": schema.Int64Attribute{Computed: true},
			"memory_mb":      schema.Int64Attribute{Computed: true},
			"tags": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
		},
	}
}

func (r *dbpgResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func setState(ctx context.Context, m *dbpgResourceModel, p *client.DbPgInstance) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.Region = types.StringValue(p.Region)
	m.VpcID = types.StringValue(p.VpcID)
	m.VnetID = types.StringValue(p.VnetID)
	m.Plan = types.StringValue(p.Plan)
	m.StorageGB = types.Int64Value(int64(p.StorageGB))
	m.Replicas = types.Int64Value(int64(p.Replicas))
	m.Tier = types.StringValue(p.Tier)
	if p.EngineVersion != nil {
		m.EngineVersion = types.StringValue(*p.EngineVersion)
	} else {
		m.EngineVersion = types.StringNull()
	}
	m.Status = types.StringValue(p.Status)
	if p.EndpointVnetIP != nil {
		m.EndpointVnetIP = types.StringValue(*p.EndpointVnetIP)
	} else {
		m.EndpointVnetIP = types.StringNull()
	}
	if p.EndpointPort != nil {
		m.EndpointPort = types.Int64Value(int64(*p.EndpointPort))
	} else {
		m.EndpointPort = types.Int64Null()
	}
	if p.AdminUsername != nil {
		m.AdminUsername = types.StringValue(*p.AdminUsername)
	} else {
		m.AdminUsername = types.StringNull()
	}
	if p.AdminDatabase != nil {
		m.AdminDatabase = types.StringValue(*p.AdminDatabase)
	} else {
		m.AdminDatabase = types.StringNull()
	}
	m.CPUMillicores = types.Int64Value(int64(p.CPUMillicores))
	m.MemoryMB = types.Int64Value(int64(p.MemoryMB))
	tags, _ := types.ListValueFrom(ctx, types.StringType, p.Tags)
	m.Tags = tags
}

func (r *dbpgResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dbpgResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createReq := client.DbPgInstanceCreateRequest{
		Name:      plan.Name.ValueString(),
		Region:    plan.Region.ValueString(),
		VpcID:     plan.VpcID.ValueString(),
		VnetID:    plan.VnetID.ValueString(),
		Plan:      plan.Plan.ValueString(),
		StorageGB: int(plan.StorageGB.ValueInt64()),
	}
	if !plan.Replicas.IsNull() && !plan.Replicas.IsUnknown() {
		v := int(plan.Replicas.ValueInt64())
		createReq.Replicas = &v
	}
	if !plan.EngineVersion.IsNull() && !plan.EngineVersion.IsUnknown() {
		v := plan.EngineVersion.ValueString()
		createReq.EngineVersion = &v
	}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags := []string{}
		plan.Tags.ElementsAs(ctx, &tags, false)
		createReq.Tags = tags
	}
	created, err := r.client.CreateDbPg(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create PostgreSQL instance", err.Error())
		return
	}
	final, err := pollUntilActive(ctx, r.client, created.ID, 10*time.Minute)
	if err != nil {
		resp.Diagnostics.AddError("PostgreSQL provisioning timed out or failed", err.Error())
		return
	}
	setState(ctx, &plan, final)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dbpgResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dbpgResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.client.GetDbPg(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read PostgreSQL instance", err.Error())
		return
	}
	setState(ctx, &state, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dbpgResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported",
		"All ccp_db_pg_instance attributes force replacement in v1.")
}

func (r *dbpgResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dbpgResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteDbPg(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete PostgreSQL instance", err.Error())
	}
}

func (r *dbpgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func pollUntilActive(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.DbPgInstance, error) {
	deadline := time.Now().Add(timeout)
	for {
		inst, err := c.GetDbPg(ctx, id)
		if err != nil {
			return nil, err
		}
		if inst.Status == "active" {
			return inst, nil
		}
		if inst.Status == "error" {
			msg := "unknown"
			if inst.ErrorMessage != nil {
				msg = *inst.ErrorMessage
			}
			return inst, fmt.Errorf("entered error state: %s", msg)
		}
		if time.Now().After(deadline) {
			return inst, fmt.Errorf("polling timeout (last status: %s)", inst.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}
