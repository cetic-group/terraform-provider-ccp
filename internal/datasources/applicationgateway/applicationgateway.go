// Package applicationgateway implements the ccp_application_gateway
// data source — look up an existing Application Gateway by `id` or by
// `(name, region)`.
package applicationgateway

import (
	"context"
	"fmt"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*appgwDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*appgwDataSource)(nil)
)

func New() datasource.DataSource { return &appgwDataSource{} }

type appgwDataSource struct{ client *client.Client }

type listenerModel struct {
	ID                types.String `tfsdk:"id"`
	Hostname          types.String `tfsdk:"hostname"`
	CustomDomain      types.Bool   `tfsdk:"custom_domain"`
	AcmeStatus        types.String `tfsdk:"acme_status"`
	AcmeLastRenewalAt types.String `tfsdk:"acme_last_renewal_at"`
}

type targetGroupModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Algorithm types.String `tfsdk:"algorithm"`
}

type routeModel struct {
	ID            types.String `tfsdk:"id"`
	ListenerID    types.String `tfsdk:"listener_id"`
	Priority      types.Int64  `tfsdk:"priority"`
	PathMatch     types.String `tfsdk:"path_match"`
	PathMatchType types.String `tfsdk:"path_match_type"`
	TargetGroupID types.String `tfsdk:"target_group_id"`
}

type appgwDSModel struct {
	ID                    types.String       `tfsdk:"id"`
	Name                  types.String       `tfsdk:"name"`
	Region                types.String       `tfsdk:"region"`
	Plan                  types.String       `tfsdk:"plan"`
	VpcID                 types.String       `tfsdk:"vpc_id"`
	VnetID                types.String       `tfsdk:"vnet_id"`
	PublicIPID            types.String       `tfsdk:"public_ip_id"`
	PublicIPAddress       types.String       `tfsdk:"public_ip_address"`
	PublicIPStatus        types.String       `tfsdk:"public_ip_status"`
	VIPAddress            types.String       `tfsdk:"vip_address"`
	Status                types.String       `tfsdk:"status"`
	ErrorMessage          types.String       `tfsdk:"error_message"`
	ForceHTTPS            types.Bool         `tfsdk:"force_https"`
	HSTSEnabled           types.Bool         `tfsdk:"hsts_enabled"`
	HSTSMaxAge            types.Int64        `tfsdk:"hsts_max_age"`
	GlobalRateLimitPerSec types.Int64        `tfsdk:"global_rate_limit_per_sec"`
	GlobalAllowCIDRs      types.List         `tfsdk:"global_allow_cidrs"`
	GlobalDenyCIDRs       types.List         `tfsdk:"global_deny_cidrs"`
	TrustProxyHeaders     types.Bool         `tfsdk:"trust_proxy_headers"`
	Tags                  types.List         `tfsdk:"tags"`
	CreatedAt             types.String       `tfsdk:"created_at"`
	Listeners             []listenerModel    `tfsdk:"listeners"`
	TargetGroups          []targetGroupModel `tfsdk:"target_groups"`
	Routes                []routeModel       `tfsdk:"routes"`
}

func (d *appgwDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_gateway"
}

func (d *appgwDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing CETIC Cloud Application Gateway by `id` or by `(name, region)`. " +
			"Exactly one of those discriminators must be provided.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "UUID of the gateway. Conflicts with `name` + `region`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the gateway. Combined with `region` to identify it.",
				Optional:            true,
				Computed:            true,
			},
			"region":                   schema.StringAttribute{Optional: true, Computed: true},
			"plan":                     schema.StringAttribute{Computed: true},
			"vpc_id":                   schema.StringAttribute{Computed: true},
			"vnet_id":                  schema.StringAttribute{Computed: true},
			"public_ip_id":             schema.StringAttribute{Computed: true},
			"public_ip_address":        schema.StringAttribute{Computed: true},
			"public_ip_status":         schema.StringAttribute{Computed: true},
			"vip_address":              schema.StringAttribute{Computed: true},
			"status":                   schema.StringAttribute{Computed: true},
			"error_message":            schema.StringAttribute{Computed: true},
			"force_https":              schema.BoolAttribute{Computed: true},
			"hsts_enabled":             schema.BoolAttribute{Computed: true},
			"hsts_max_age":             schema.Int64Attribute{Computed: true},
			"global_rate_limit_per_sec": schema.Int64Attribute{Computed: true},
			"global_allow_cidrs":       schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"global_deny_cidrs":        schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"trust_proxy_headers":      schema.BoolAttribute{Computed: true},
			"tags":                     schema.ListAttribute{ElementType: types.StringType, Computed: true},
			"created_at":               schema.StringAttribute{Computed: true},
			"listeners": schema.ListNestedAttribute{
				MarkdownDescription: "Listeners (hostnames + ACME state) attached to the gateway.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                   schema.StringAttribute{Computed: true},
						"hostname":             schema.StringAttribute{Computed: true},
						"custom_domain":        schema.BoolAttribute{Computed: true},
						"acme_status":          schema.StringAttribute{Computed: true},
						"acme_last_renewal_at": schema.StringAttribute{Computed: true},
					},
				},
			},
			"target_groups": schema.ListNestedAttribute{
				MarkdownDescription: "Target groups (backend pools) defined on the gateway.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":        schema.StringAttribute{Computed: true},
						"name":      schema.StringAttribute{Computed: true},
						"algorithm": schema.StringAttribute{Computed: true},
					},
				},
			},
			"routes": schema.ListNestedAttribute{
				MarkdownDescription: "Routes (condition + policies) defined on the gateway.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":              schema.StringAttribute{Computed: true},
						"listener_id":     schema.StringAttribute{Computed: true},
						"priority":        schema.Int64Attribute{Computed: true},
						"path_match":      schema.StringAttribute{Computed: true},
						"path_match_type": schema.StringAttribute{Computed: true},
						"target_group_id": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *appgwDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *appgwDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg appgwDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasName := !cfg.Name.IsNull() && !cfg.Name.IsUnknown() && cfg.Name.ValueString() != ""
	hasRegion := !cfg.Region.IsNull() && !cfg.Region.IsUnknown() && cfg.Region.ValueString() != ""

	switch {
	case hasID && (hasName || hasRegion):
		resp.Diagnostics.AddError("Conflicting lookup arguments",
			"Provide either `id`, or both `name` and `region` — not both.")
		return
	case !hasID && !(hasName && hasRegion):
		resp.Diagnostics.AddError("Missing lookup arguments",
			"Provide either `id`, or both `name` and `region`.")
		return
	}

	var found *client.ApplicationGateway
	if hasID {
		got, err := d.client.GetApplicationGateway(ctx, cfg.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to read Application Gateway", err.Error())
			return
		}
		found = got
	} else {
		list, err := d.client.ListApplicationGateways(ctx, cfg.Region.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to list Application Gateways", err.Error())
			return
		}
		wantName := cfg.Name.ValueString()
		for i := range list {
			if list[i].Name == wantName {
				got, err := d.client.GetApplicationGateway(ctx, list[i].ID)
				if err != nil {
					resp.Diagnostics.AddError("Failed to fetch Application Gateway detail", err.Error())
					return
				}
				found = got
				break
			}
		}
		if found == nil {
			resp.Diagnostics.AddError("Application Gateway not found",
				fmt.Sprintf("No gateway named %q in region %q.", wantName, cfg.Region.ValueString()))
			return
		}
	}

	state := appgwDSModel{
		ID:                types.StringValue(found.ID),
		Name:              types.StringValue(found.Name),
		Region:            types.StringValue(found.Region),
		Plan:              types.StringValue(found.Plan),
		VpcID:             types.StringValue(found.VpcID),
		VnetID:            types.StringValue(found.VnetID),
		Status:            types.StringValue(found.Status),
		ForceHTTPS:        types.BoolValue(found.ForceHTTPS),
		HSTSEnabled:       types.BoolValue(found.HSTSEnabled),
		HSTSMaxAge:        types.Int64Value(found.HSTSMaxAge),
		TrustProxyHeaders: types.BoolValue(found.TrustProxyHeaders),
		CreatedAt:         types.StringValue(found.CreatedAt),
	}
	if found.PublicIPID != nil {
		state.PublicIPID = types.StringValue(*found.PublicIPID)
	} else {
		state.PublicIPID = types.StringNull()
	}
	if found.PublicIPAddress != nil {
		state.PublicIPAddress = types.StringValue(*found.PublicIPAddress)
	} else {
		state.PublicIPAddress = types.StringNull()
	}
	if found.PublicIPStatus != nil {
		state.PublicIPStatus = types.StringValue(*found.PublicIPStatus)
	} else {
		state.PublicIPStatus = types.StringNull()
	}
	if found.VIPAddress != nil {
		state.VIPAddress = types.StringValue(*found.VIPAddress)
	} else {
		state.VIPAddress = types.StringNull()
	}
	if found.ErrorMessage != nil {
		state.ErrorMessage = types.StringValue(*found.ErrorMessage)
	} else {
		state.ErrorMessage = types.StringNull()
	}
	if found.GlobalRateLimitPerSec != nil {
		state.GlobalRateLimitPerSec = types.Int64Value(*found.GlobalRateLimitPerSec)
	} else {
		state.GlobalRateLimitPerSec = types.Int64Null()
	}
	allow, _ := types.ListValueFrom(ctx, types.StringType, found.GlobalAllowCIDRs)
	state.GlobalAllowCIDRs = allow
	deny, _ := types.ListValueFrom(ctx, types.StringType, found.GlobalDenyCIDRs)
	state.GlobalDenyCIDRs = deny
	tags, _ := types.ListValueFrom(ctx, types.StringType, found.Tags)
	state.Tags = tags

	// Listeners
	for _, l := range found.Listeners {
		lm := listenerModel{
			ID:           types.StringValue(l.ID),
			Hostname:     types.StringValue(l.Hostname),
			CustomDomain: types.BoolValue(l.CustomDomain),
			AcmeStatus:   types.StringValue(l.AcmeStatus),
		}
		if l.AcmeLastRenewalAt != nil {
			lm.AcmeLastRenewalAt = types.StringValue(*l.AcmeLastRenewalAt)
		} else {
			lm.AcmeLastRenewalAt = types.StringNull()
		}
		state.Listeners = append(state.Listeners, lm)
	}
	for _, tg := range found.TargetGroups {
		state.TargetGroups = append(state.TargetGroups, targetGroupModel{
			ID:        types.StringValue(tg.ID),
			Name:      types.StringValue(tg.Name),
			Algorithm: types.StringValue(tg.Algorithm),
		})
	}
	for _, rt := range found.Routes {
		rm := routeModel{
			ID:            types.StringValue(rt.ID),
			ListenerID:    types.StringValue(rt.ListenerID),
			Priority:      types.Int64Value(rt.Priority),
			PathMatchType: types.StringValue(rt.PathMatchType),
			TargetGroupID: types.StringValue(rt.TargetGroupID),
		}
		if rt.PathMatch != nil {
			rm.PathMatch = types.StringValue(*rt.PathMatch)
		} else {
			rm.PathMatch = types.StringNull()
		}
		state.Routes = append(state.Routes, rm)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
