// Package promocodes provides the ccp_promo_codes_available data source
// — lists public promo codes currently usable by the tenant.
package promocodes

import (
	"context"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &promoCodesDataSource{}
	_ datasource.DataSourceWithConfigure = &promoCodesDataSource{}
)

func New() datasource.DataSource { return &promoCodesDataSource{} }

type promoCodesDataSource struct{ client *client.Client }

type promoCodesModel struct {
	Codes []promoCodeModel `tfsdk:"codes"`
}

type promoCodeModel struct {
	ID             types.String `tfsdk:"id"`
	Code           types.String `tfsdk:"code"`
	Description    types.String `tfsdk:"description"`
	DiscountPct    types.Int64  `tfsdk:"discount_pct"`
	DurationMonths types.Int64  `tfsdk:"duration_months"`
}

func (d *promoCodesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_promo_codes_available"
}

func (d *promoCodesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists publicly-available promo codes the current tenant can apply. Useful to display in a portal or to programmatically apply on signup.",
		Attributes: map[string]schema.Attribute{
			"codes": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":              schema.StringAttribute{Computed: true},
						"code":            schema.StringAttribute{Computed: true, Description: "Uppercase code (ex: `LAUNCH2026`)."},
						"description":     schema.StringAttribute{Computed: true},
						"discount_pct":    schema.Int64Attribute{Computed: true, Description: "Discount in percent (1-100)."},
						"duration_months": schema.Int64Attribute{Computed: true, Description: "How many months the discount stays active for the tenant after apply."},
					},
				},
			},
		},
	}
}

func (d *promoCodesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *promoCodesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	items, err := d.client.ListAvailablePromoCodes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("ListAvailablePromoCodes failed", err.Error())
		return
	}
	out := make([]promoCodeModel, 0, len(items))
	for _, p := range items {
		out = append(out, promoCodeModel{
			ID:             types.StringValue(p.ID),
			Code:           types.StringValue(p.Code),
			Description:    types.StringValue(p.Description),
			DiscountPct:    types.Int64Value(int64(p.DiscountPct)),
			DurationMonths: types.Int64Value(int64(p.DurationMonths)),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &promoCodesModel{Codes: out})...)
}
