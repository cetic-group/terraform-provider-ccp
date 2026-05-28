// Package supportplan provides the ccp_support_plan datasource — exposes
// the catalogue entry for a given support plan key (vague C6).
package supportplan

import (
	"context"
	"strconv"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ datasource.DataSource              = &supportPlanDataSource{}
	_ datasource.DataSourceWithConfigure = &supportPlanDataSource{}
)

func New() datasource.DataSource { return &supportPlanDataSource{} }

type supportPlanDataSource struct{ client *client.Client }

type supportPlanModel struct {
	ID                    types.String `tfsdk:"id"`
	Key                   types.String `tfsdk:"key"`
	DisplayName           types.String `tfsdk:"display_name"`
	Description           types.String `tfsdk:"description"`
	PriceEurMonthCents    types.Int64  `tfsdk:"price_eur_month_cents"`
	PriceEurMonth         types.Float64 `tfsdk:"price_eur_month"`
	SlaFirstResponseHours types.Int64  `tfsdk:"sla_first_response_hours"`
	SlaResolutionHours    types.Int64  `tfsdk:"sla_resolution_hours"`
	MaxPriority           types.String `tfsdk:"max_priority"`
	Channels              types.List   `tfsdk:"channels"`
	IsDefault             types.Bool   `tfsdk:"is_default"`
	IsActive              types.Bool   `tfsdk:"is_active"`
	Features              types.Map    `tfsdk:"features"`
}

func (d *supportPlanDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_support_plan"
}

func (d *supportPlanDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Read-only catalogue entry for a CETIC support plan (vague C6). Use this to derive SLA / price / channels in modules without hardcoding.",
		Attributes: map[string]schema.Attribute{
			"id":  schema.StringAttribute{Computed: true},
			"key": schema.StringAttribute{Required: true, Description: "Plan key (e.g. `base`, `standard`, `premium`)."},
			"display_name": schema.StringAttribute{Computed: true},
			"description":  schema.StringAttribute{Computed: true},
			"price_eur_month_cents": schema.Int64Attribute{
				Computed:    true,
				Description: "Monthly price in EUR cents (0 for free plans).",
			},
			"price_eur_month": schema.Float64Attribute{
				Computed:    true,
				Description: "Monthly price in EUR (convenience, derived from cents).",
			},
			"sla_first_response_hours": schema.Int64Attribute{
				Computed:    true,
				Description: "First-response SLA in hours.",
			},
			"sla_resolution_hours": schema.Int64Attribute{
				Computed:    true,
				Description: "Resolution SLA in hours (0 means best-effort, no SLA).",
			},
			"max_priority": schema.StringAttribute{
				Computed:    true,
				Description: "Maximum ticket priority allowed (`low`, `normal`, `high`, `urgent`).",
			},
			"channels": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Supported support channels (`email`, `chat`, `phone`).",
			},
			"is_default": schema.BoolAttribute{Computed: true},
			"is_active":  schema.BoolAttribute{Computed: true},
			"features": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Free-form marketing bullets keyed by feature ID (values stringified).",
			},
		},
	}
}

func (d *supportPlanDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Configure error", "unexpected provider data type")
		return
	}
	d.client = c
}

func (d *supportPlanDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg supportPlanModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := d.client.GetSupportPlan(ctx, cfg.Key.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("GetSupportPlan failed", err.Error())
		return
	}

	state := supportPlanModel{
		ID:                    types.StringValue(p.ID),
		Key:                   types.StringValue(p.Key),
		DisplayName:           types.StringValue(p.DisplayName),
		Description:           types.StringValue(p.Description),
		PriceEurMonthCents:    types.Int64Value(int64(p.PriceEurMonthCents)),
		PriceEurMonth:         types.Float64Value(float64(p.PriceEurMonthCents) / 100.0),
		SlaFirstResponseHours: types.Int64Value(int64(p.SlaFirstResponseHours)),
		MaxPriority:           types.StringValue(p.MaxPriority),
		IsDefault:             types.BoolValue(p.IsDefault),
		IsActive:              types.BoolValue(p.IsActive),
	}
	if p.SlaResolutionHours != nil {
		state.SlaResolutionHours = types.Int64Value(int64(*p.SlaResolutionHours))
	} else {
		state.SlaResolutionHours = types.Int64Value(0)
	}
	if p.Channels == nil {
		p.Channels = []string{}
	}
	chList, _ := basetypes.NewListValueFrom(ctx, types.StringType, p.Channels)
	state.Channels = chList
	// Features map — coerce all values to string (Terraform map[string]string).
	featStrs := make(map[string]string, len(p.Features))
	for k, v := range p.Features {
		switch t := v.(type) {
		case string:
			featStrs[k] = t
		case bool:
			if t {
				featStrs[k] = "true"
			} else {
				featStrs[k] = "false"
			}
		case float64:
			featStrs[k] = formatFloat(t)
		default:
			featStrs[k] = ""
		}
	}
	featMap, _ := basetypes.NewMapValueFrom(ctx, types.StringType, featStrs)
	state.Features = featMap

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
