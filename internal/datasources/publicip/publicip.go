// Package publicip implements the ccp_public_ip data source.
package publicip

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*pipDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*pipDataSource)(nil)
)

func New() datasource.DataSource { return &pipDataSource{} }

type pipDataSource struct{ client *client.Client }

type pipDSModel struct {
	ID               types.String `tfsdk:"id"`
	IPAddress        types.String `tfsdk:"ip_address"`
	PoolID           types.String `tfsdk:"pool_id"`
	Region           types.String `tfsdk:"region"`
	Status           types.String `tfsdk:"status"`
	ContainerID      types.String `tfsdk:"container_id"`
	VMInstanceID     types.String `tfsdk:"vm_instance_id"`
	LoadBalancerID   types.String `tfsdk:"load_balancer_id"`
	LoadBalancerName types.String `tfsdk:"load_balancer_name"`
	Label            types.String `tfsdk:"label"`
	Description      types.String `tfsdk:"description"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (d *pipDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp_public_ip"
}

func (d *pipDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up a Public IP by `id`, `ip_address`, or `label`. Provide exactly one. " +
			"Labels are not guaranteed unique — if more than one Public IP carries the same `label`, " +
			"the lookup fails with an explicit error and you must disambiguate with `id` or `ip_address`.",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Optional: true, Computed: true},
			"ip_address":         schema.StringAttribute{Optional: true, Computed: true},
			"pool_id":            schema.StringAttribute{Computed: true},
			"region":             schema.StringAttribute{Computed: true},
			"status":             schema.StringAttribute{Computed: true},
			"container_id":       schema.StringAttribute{Computed: true},
			"vm_instance_id":     schema.StringAttribute{Computed: true},
			"load_balancer_id":   schema.StringAttribute{Computed: true},
			"load_balancer_name": schema.StringAttribute{Computed: true},
			"label":              schema.StringAttribute{Optional: true, Computed: true},
			"description":        schema.StringAttribute{Computed: true},
			"created_at":         schema.StringAttribute{Computed: true},
		},
	}
}

func (d *pipDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *pipDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg pipDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown() && cfg.ID.ValueString() != ""
	hasAddr := !cfg.IPAddress.IsNull() && !cfg.IPAddress.IsUnknown() && cfg.IPAddress.ValueString() != ""
	hasLabel := !cfg.Label.IsNull() && !cfg.Label.IsUnknown() && cfg.Label.ValueString() != ""

	n := 0
	for _, b := range []bool{hasID, hasAddr, hasLabel} {
		if b {
			n++
		}
	}
	if n != 1 {
		resp.Diagnostics.AddError("Lookup arguments", "Provide exactly one of `id`, `ip_address` or `label`.")
		return
	}

	list, err := d.client.ListPublicIPs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list public IPs", err.Error())
		return
	}

	found, summary, detail := selectPublicIP(list, cfg.ID.ValueString(), cfg.IPAddress.ValueString(), cfg.Label.ValueString(), hasID, hasAddr, hasLabel)
	if found == nil {
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	state := pipDSModel{
		ID:        types.StringValue(found.ID),
		IPAddress: types.StringValue(found.IPAddress),
		PoolID:    types.StringValue(found.PoolID),
		Region:    types.StringValue(found.Region),
		Status:    types.StringValue(found.Status),
		CreatedAt: types.StringValue(found.CreatedAt.Format(time.RFC3339)),
	}
	setStrPtr(&state.ContainerID, found.ContainerID)
	setStrPtr(&state.VMInstanceID, found.VMInstanceID)
	setStrPtr(&state.LoadBalancerID, found.LoadBalancerID)
	setStrPtr(&state.LoadBalancerName, found.LoadBalancerName)
	setStrPtr(&state.Label, found.Label)
	setStrPtr(&state.Description, found.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// selectPublicIP picks the single Public IP matching the active lookup key.
// Exactly one of hasID/hasAddr/hasLabel must be true (enforced by the caller).
// It returns (nil, summary, detail) describing an error diagnostic when no IP
// matches or, for label lookups, when more than one IP shares the label.
func selectPublicIP(list []client.PublicIP, id, addr, label string, hasID, hasAddr, hasLabel bool) (*client.PublicIP, string, string) {
	if hasLabel {
		var match *client.PublicIP
		count := 0
		for i := range list {
			if list[i].Label != nil && *list[i].Label == label {
				count++
				if match == nil {
					match = &list[i]
				}
			}
		}
		switch count {
		case 0:
			return nil, "Public IP not found", fmt.Sprintf("No public IP matches label %q.", label)
		case 1:
			return match, "", ""
		default:
			return nil, "Ambiguous lookup", fmt.Sprintf("Multiple public IPs match label %q — labels are not unique; use `id` or `ip_address` instead.", label)
		}
	}

	for i := range list {
		if (hasID && list[i].ID == id) || (hasAddr && list[i].IPAddress == addr) {
			return &list[i], "", ""
		}
	}
	return nil, "Public IP not found", "No matching public IP."
}

func setStrPtr(dst *types.String, src *string) {
	if src != nil {
		*dst = types.StringValue(*src)
	} else {
		*dst = types.StringNull()
	}
}
