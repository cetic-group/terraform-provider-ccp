// Package pricing provides the ccp_pricing data source — reads the live
// pricing grid from the platform (admin-edited via backoffice).
package pricing

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &pricingDataSource{}
	_ datasource.DataSourceWithConfigure = &pricingDataSource{}
)

func New() datasource.DataSource { return &pricingDataSource{} }

type pricingDataSource struct {
	client *client.Client
}

type pricingModel struct {
	ResourceType types.String       `tfsdk:"resource_type"`
	Plan         types.String       `tfsdk:"plan"`
	Items        []pricingItemModel `tfsdk:"items"`
}

type pricingItemModel struct {
	ID                               types.String  `tfsdk:"id"`
	ResourceType                     types.String  `tfsdk:"resource_type"`
	Plan                             types.String  `tfsdk:"plan"`
	HourlyPriceCents                 types.Int64   `tfsdk:"hourly_price_cents"`
	MonthlyPriceEUR                  types.Float64 `tfsdk:"monthly_price_eur"`
	YearlyPriceEUR                   types.Float64 `tfsdk:"yearly_price_eur"`
	Currency                         types.String  `tfsdk:"currency"`
	Description                      types.String  `tfsdk:"description"`
	IsFree                           types.Bool    `tfsdk:"is_free"`
	BillingDimension                 types.String  `tfsdk:"billing_dimension"`
	StoppedDiskPriceCentsPerGBHour   types.Int64   `tfsdk:"stopped_disk_price_cents_per_gb_hour"`
	MonthlyCommitDiscountPct         types.Int64   `tfsdk:"monthly_commit_discount_pct"`
	YearlyCommitDiscountPct          types.Int64   `tfsdk:"yearly_commit_discount_pct"`
}

func (d *pricingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_pricing"
}

func (d *pricingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the live pricing grid (`resource_pricing`). Filter by `resource_type` and/or `plan` to narrow the result. Useful to compute cost estimates from HCL or to gate resource creation on a budget computation.",
		Attributes: map[string]schema.Attribute{
			"resource_type": schema.StringAttribute{
				Description: "Optional filter on resource type (ex: `container`, `vm`, `block_volume`, `db_instance`, `k8s_cluster_hcp`, `k8s_node`, `registry`, `appgw`, `public_ip`, `load_balancer`, `vnet_peering`, `object_storage`, `snapshot`, `template`).",
				Optional:    true,
			},
			"plan": schema.StringAttribute{
				Description: "Optional filter on plan (ex: `nano`, `small`, `prod:medium`). Combined with `resource_type`.",
				Optional:    true,
			},
			"items": schema.ListNestedAttribute{
				Description: "Active pricing rows matching the filter (or all if no filter).",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                schema.StringAttribute{Computed: true, Description: "UUID of the pricing row."},
						"resource_type":     schema.StringAttribute{Computed: true},
						"plan":              schema.StringAttribute{Computed: true, Description: "Plan key or null for flat-priced resources."},
						"hourly_price_cents": schema.Int64Attribute{Computed: true, Description: "Price in cents per hour."},
						"monthly_price_eur":  schema.Float64Attribute{Computed: true, Description: "Hourly price × 730 hours / 100."},
						"yearly_price_eur":   schema.Float64Attribute{Computed: true, Description: "Hourly price × 8760 hours / 100."},
						"currency":           schema.StringAttribute{Computed: true},
						"description":        schema.StringAttribute{Computed: true},
						"is_free":            schema.BoolAttribute{Computed: true, Description: "True if this resource is marked free (collector skips billing)."},
						"billing_dimension":  schema.StringAttribute{Computed: true, Description: "`flat_hourly`, `per_gb_hourly`, `per_gb_egress`, or `per_million_requests`."},
						"stopped_disk_price_cents_per_gb_hour": schema.Int64Attribute{Computed: true, Description: "When set, stopped instances of this type are billed at this price per GB of disk per hour, instead of the full plan rate."},
						"monthly_commit_discount_pct":          schema.Int64Attribute{Computed: true, Description: "Discount applied when a monthly commit is active."},
						"yearly_commit_discount_pct":           schema.Int64Attribute{Computed: true, Description: "Discount applied when a yearly commit is active."},
					},
				},
			},
		},
	}
}

func (d *pricingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *pricingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data pricingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	all, err := d.client.ListPricing(ctx)
	if err != nil {
		resp.Diagnostics.AddError("ListPricing failed", err.Error())
		return
	}

	wantType := data.ResourceType.ValueString()
	wantPlan := data.Plan.ValueString()
	out := make([]pricingItemModel, 0, len(all))
	for _, p := range all {
		if wantType != "" && p.ResourceType != wantType {
			continue
		}
		if wantPlan != "" {
			if p.Plan == nil || *p.Plan != wantPlan {
				continue
			}
		}
		item := pricingItemModel{
			ID:                       types.StringValue(p.ID),
			ResourceType:             types.StringValue(p.ResourceType),
			Plan:                     stringOrNull(p.Plan),
			HourlyPriceCents:         types.Int64Value(int64(p.HourlyPriceCents)),
			MonthlyPriceEUR:          types.Float64Value(p.MonthlyPriceEUR),
			YearlyPriceEUR:           types.Float64Value(p.YearlyPriceEUR),
			Currency:                 types.StringValue(p.Currency),
			Description:              stringOrNull(p.Description),
			IsFree:                   types.BoolValue(p.IsFree),
			BillingDimension:         types.StringValue(p.BillingDimension),
			MonthlyCommitDiscountPct: types.Int64Value(int64(p.MonthlyCommitDiscountPct)),
			YearlyCommitDiscountPct:  types.Int64Value(int64(p.YearlyCommitDiscountPct)),
		}
		if p.StoppedDiskPriceCentsPerGBHour != nil {
			item.StoppedDiskPriceCentsPerGBHour = types.Int64Value(int64(*p.StoppedDiskPriceCentsPerGBHour))
		} else {
			item.StoppedDiskPriceCentsPerGBHour = types.Int64Null()
		}
		out = append(out, item)
	}
	data.Items = out
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	_ = fmt.Sprintf // keep import alive if needed
}

func stringOrNull(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}
