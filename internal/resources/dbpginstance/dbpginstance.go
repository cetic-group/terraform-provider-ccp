// Package dbpginstance implements the ccp_db_pg_instance Terraform resource.
//
// Instance PostgreSQL managée. Provisioning asynchrone — Create poll jusqu'à
// status=active. Seuls les réglages d'extensions se modifient en place
// (extensions.go) ; le reste force un replace ou est refusé en Update.
package dbpginstance

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*dbpgResource)(nil)
	_ resource.ResourceWithConfigure   = (*dbpgResource)(nil)
	_ resource.ResourceWithImportState = (*dbpgResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*dbpgResource)(nil)
)

func New() resource.Resource { return &dbpgResource{} }

type dbpgResource struct{ client *client.Client }

type dbpgResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Region         types.String `tfsdk:"region"`
	VpcID          types.String `tfsdk:"vpc_id"`
	VnetID         types.String `tfsdk:"vnet_id"`
	Plan           types.String `tfsdk:"plan"`
	StorageGB      types.Int64  `tfsdk:"storage_gb"`
	Replicas       types.Int64  `tfsdk:"replicas"`
	Tier           types.String `tfsdk:"tier"`
	EngineVersion  types.String `tfsdk:"engine_version"`
	Status         types.String `tfsdk:"status"`
	EndpointVnetIP types.String `tfsdk:"endpoint_vnet_ip"`
	EndpointPort   types.Int64  `tfsdk:"endpoint_port"`
	AdminUsername  types.String `tfsdk:"admin_username"`
	AdminDatabase  types.String `tfsdk:"admin_database"`
	CPUMillicores  types.Int64  `tfsdk:"cpu_millicores"`
	MemoryMB       types.Int64  `tfsdk:"memory_mb"`
	Tags           types.List   `tfsdk:"tags"`
	// Extensions (extensions.go).
	ExtensionsAvailable types.Bool   `tfsdk:"extensions_available"`
	AllowedExtensions   types.Set    `tfsdk:"allowed_extensions"`
	PreloadLibraries    types.Set    `tfsdk:"preload_libraries"`
	CronDatabase        types.String `tfsdk:"cron_database"`
	ExtensionsStatus    types.String `tfsdk:"extensions_status"`
}

func (r *dbpgResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "ccp_db_pg_instance"
}

func (r *dbpgResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "CETIC Cloud managed PostgreSQL instance. Provisioning is async " +
			"(~5-10 min). Extension settings (`allowed_extensions`, `preload_libraries`, " +
			"`cron_database`) are updated in place; the other attributes cannot change — " +
			"to scale, recreate the instance.",
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
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"tier": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"engine_version": schema.StringAttribute{
				MarkdownDescription: "Major version (default '16'). Versions exposées sont gérées via le backoffice CETIC.",
				Optional:            true,
				Computed:            true,
				// UseStateForUnknown AVANT RequiresReplace : sans lui, une valeur
				// omise redevient inconnue dès qu'un autre attribut change, et
				// RequiresReplace propose de recréer l'instance — une simple
				// modification d'extensions détruirait la base.
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
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
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"admin_username": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"admin_database": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"cpu_millicores": schema.Int64Attribute{
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"memory_mb": schema.Int64Attribute{
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"tags": schema.ListAttribute{
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"extensions_available": schema.BoolAttribute{
				MarkdownDescription: "Whether extensions can be allowed on this instance. They must " +
					"first be enabled on the instance by support; until then, extension settings " +
					"cannot change.",
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"allowed_extensions": schema.SetAttribute{
				MarkdownDescription: "Extensions from the catalog (`ccp_db_pg_extensions`) that the " +
					"database owner may then create with `CREATE EXTENSION`. List the extensions " +
					"they depend on as well. Extensions that need no authorization (see " +
					"`direct_extensions` of the catalog) are not listed here. Removing an " +
					"extension is refused while it is installed in a database: drop it first. " +
					"Omitted: left unchanged; `[]`: none allowed.",
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"preload_libraries": schema.SetAttribute{
				MarkdownDescription: "Libraries loaded at startup, required by some extensions " +
					"(`pg_cron`, `pg_stat_statements`). Changing this list restarts the instance, " +
					"one replica at a time. Omitted: left unchanged; `[]`: none.",
				ElementType:   types.StringType,
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"cron_database": schema.StringAttribute{
				MarkdownDescription: "Database in which `pg_cron` runs scheduled jobs. Requires " +
					"`pg_cron` in `preload_libraries`. Defaults to the instance's initial database. " +
					"Omitted: left unchanged.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"extensions_status": schema.StringAttribute{
				MarkdownDescription: "`applied` when the instance runs with its extension settings, " +
					"`pending` while a change is being applied. `terraform apply` waits for " +
					"`applied`.",
				Computed: true,
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
	m.ExtensionsAvailable = types.BoolValue(p.ExtensionsAvailable)
	allowed, _ := types.SetValueFrom(ctx, types.StringType, nonNil(p.AllowedExtensions))
	m.AllowedExtensions = allowed
	preload, _ := types.SetValueFrom(ctx, types.StringType, nonNil(p.PreloadLibraries))
	m.PreloadLibraries = preload
	if p.CronDatabase != nil {
		m.CronDatabase = types.StringValue(*p.CronDatabase)
	} else {
		m.CronDatabase = types.StringNull()
	}
	status := p.ExtensionsStatus
	if status == "" {
		status = "applied"
	}
	m.ExtensionsStatus = types.StringValue(status)
}

// nonNil maps a missing list to an empty set rather than null, so that an
// explicit `[]` in the configuration matches the state.
func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
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

	// The remote object exists from here on. Persist it before waiting: if
	// provisioning fails, Terraform keeps the resource (tainted) and replaces it
	// on the next apply. Dropped from state it would be neither destroyed nor
	// re-planned, and the next apply would strand it and create a second one.
	// The projection runs on a COPY: `plan` still carries the practitioner's
	// intent, which later code reads to decide follow-up calls (VNet isolation,
	// public IP attach). Overwriting it here would silently drop that intent.
	early := plan
	setState(ctx, &early, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &early)...)
	if resp.Diagnostics.HasError() {
		return
	}
	final, err := pollUntilActive(ctx, r.client, created.ID, provisionTimeout(plan.Replicas))
	if err != nil {
		resp.Diagnostics.AddError("PostgreSQL provisioning timed out or failed", err.Error())
		return
	}
	planned := plan
	final, keepPlanned := r.applyExtensionsOnCreate(ctx, planned, final, &resp.Diagnostics)
	setState(ctx, &plan, final)
	if keepPlanned {
		keepPlannedExtensions(&plan, planned)
	}
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

func (r *dbpgResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state dbpgResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Only extension settings change in place. An unknown planned value is a
	// computed attribute left to the platform, not a change.
	changedOutside := func(p, s attr.Value) bool {
		return !p.IsUnknown() && !p.Equal(s)
	}
	if !plan.StorageGB.Equal(state.StorageGB) ||
		changedOutside(plan.Replicas, state.Replicas) ||
		changedOutside(plan.Tags, state.Tags) {
		resp.Diagnostics.AddError("Update not supported",
			"Only allowed_extensions, preload_libraries and cron_database can change in place. "+
				"Revert the other changes, or recreate the instance.")
		return
	}

	id := state.ID.ValueString()
	var final *client.DbPgInstance
	var err error
	if extReq, send := extensionRequest(ctx, plan, &state); send {
		if _, err = r.client.UpdateDbPgExtensions(ctx, id, extReq); err != nil {
			resp.Diagnostics.AddError("Failed to update PostgreSQL extensions", err.Error())
			return
		}
		if final, err = waitExtensionsApplied(ctx, r.client, id); err != nil {
			resp.Diagnostics.AddError("PostgreSQL extension settings not applied", err.Error())
			return
		}
	} else if final, err = r.client.GetDbPg(ctx, id); err != nil {
		resp.Diagnostics.AddError("Failed to read PostgreSQL instance", err.Error())
		return
	}
	setState(ctx, &plan, final)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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
		return
	}
	if err := client.PollUntilDeleted(ctx, 20*time.Minute, func(ctx context.Context) error {
		_, e := r.client.GetDbPg(ctx, state.ID.ValueString())
		return e
	}); err != nil {
		resp.Diagnostics.AddError("Failed to confirm PostgreSQL instance deletion", err.Error())
	}
}

func (r *dbpgResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// provisionTimeout scales the provisioning wait with the cluster size. Each
// replica is provisioned in turn, and a redispatch on the platform side adds
// several minutes on top. The former flat 10 minutes was shorter than a
// 3-replica HA cluster actually takes: measured at 14m40s from provision_start
// to service_ready, so an apply timed out on a provisioning that was
// progressing normally — and left the instance tainted for no good reason.
func provisionTimeout(replicas types.Int64) time.Duration {
	n := int64(1)
	if !replicas.IsNull() && !replicas.IsUnknown() && replicas.ValueInt64() > 1 {
		n = replicas.ValueInt64()
	}
	return time.Duration(15+10*(n-1)) * time.Minute
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
