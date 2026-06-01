package dbplans

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &dbPlansDataSource{}
	_ datasource.DataSourceWithConfigure = &dbPlansDataSource{}
)

func New() datasource.DataSource { return &dbPlansDataSource{} }

type dbPlansDataSource struct {
	client *client.Client
}

type dbPlansModel struct {
	Engine types.String  `tfsdk:"engine"`
	Plans  []dbPlanModel `tfsdk:"plans"`
}

type dbPlanModel struct {
	Key            types.String  `tfsdk:"key"`
	Name           types.String  `tfsdk:"name"`
	Engine         types.String  `tfsdk:"engine"`
	CPUMillicores  types.Int64   `tfsdk:"cpu_millicores"`
	MemoryMB       types.Int64   `tfsdk:"memory_mb"`
	PriceEURMonth  types.Float64 `tfsdk:"price_eur_month"`
	IsDefault      types.Bool    `tfsdk:"is_default"`
}

func (d *dbPlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_db_plans"
}

func (d *dbPlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists database plans (CPU/memory tiers) available for managed DB instances. Filter by `engine` to get plans for a specific backend.",
		Attributes: map[string]schema.Attribute{
			"engine": schema.StringAttribute{
				Description: "Optional engine filter: `pg`, `mysql`, `valkey`, `ferretdb`. If omitted, returns plans for all engines.",
				Optional:    true,
			},
			"plans": schema.ListNestedAttribute{
				Description: "List of active DB plans, possibly filtered by engine.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							Description: "Plan key (used in `ccp_db_<engine>_instance.plan`). Example: `small`, `medium`.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Human-readable plan name.",
							Computed:    true,
						},
						"engine": schema.StringAttribute{
							Description: "Engine the plan belongs to.",
							Computed:    true,
						},
						"cpu_millicores": schema.Int64Attribute{
							Description: "CPU request/limit for the plan, in millicores.",
							Computed:    true,
						},
						"memory_mb": schema.Int64Attribute{
							Description: "Memory request/limit for the plan, in mebibytes.",
							Computed:    true,
						},
						"price_eur_month": schema.Float64Attribute{
							Description: "Indicative monthly price in EUR (null if billing not configured for the plan).",
							Computed:    true,
						},
						"is_default": schema.BoolAttribute{
							Description: "Whether this plan is the default suggestion in the console.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *dbPlansDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *dbPlansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg dbPlansModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	engine := cfg.Engine.ValueString()
	plans, err := d.client.ListDbPlans(ctx, engine)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read DB plans", err.Error())
		return
	}
	state := dbPlansModel{
		Engine: cfg.Engine,
		Plans:  make([]dbPlanModel, 0, len(plans)),
	}
	for _, p := range plans {
		name := types.StringNull()
		if p.Name != nil {
			name = types.StringValue(*p.Name)
		}
		price := types.Float64Null()
		if p.PriceEURMonth != nil {
			price = types.Float64Value(*p.PriceEURMonth)
		}
		state.Plans = append(state.Plans, dbPlanModel{
			Key:           types.StringValue(p.Key),
			Name:          name,
			Engine:        types.StringValue(p.Engine),
			CPUMillicores: types.Int64Value(int64(p.CPUMillicores)),
			MemoryMB:      types.Int64Value(int64(p.MemoryMB)),
			PriceEURMonth: price,
			IsDefault:     types.BoolValue(p.IsDefault),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
