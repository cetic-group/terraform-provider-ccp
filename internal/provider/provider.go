package provider

import (
	"context"
	"os"
	"regexp"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/organizations"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/regions"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/apikey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/blockvolume"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/containerinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/containerscaleset"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/containersnapshot"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbferretdbinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbmysqlinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbpginstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbvalkeyinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/ipaaspool"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/k8scluster"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/k8snodepool"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/loadbalancer"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/objectbucket"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/objectstoragekey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/orgmember"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/organization"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/publicip"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/quotarequest"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/sshkey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/supportticket"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vminstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vmscaleset"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vmsnapshot"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnet"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnetfirewallrule"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnetipresv"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnetpeering"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vpc"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vpcpeering"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// New returns a CETIC Cloud Platform provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ccpProvider{version: version}
	}
}

type ccpProvider struct {
	version string
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

func (p *ccpProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "ccp"
	resp.Version = p.version
}

func (p *ccpProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The CETIC Cloud Platform (ccp) provider deploys infrastructure on CETIC Cloud — the sovereign cloud by CETIC Group.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "CETIC Cloud API base URL. Defaults to `https://api.cloud.cetic-group.com`. Falls back to `CCP_API_URL` env var.",
				Optional:            true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "CETIC Cloud API key (`ccp_live_*`). Falls back to `CCP_API_KEY` env var.",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

var apiKeyPattern = regexp.MustCompile(`^cl_(live|test)_[A-Za-z0-9_-]{20,}$`)

func (p *ccpProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := data.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = os.Getenv("CCP_API_URL")
	}

	apiKey := data.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("CCP_API_KEY")
	}

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing CETIC Cloud API key",
			"Set `api_key` in the provider block or export `CCP_API_KEY` (format: ccp_live_*).",
		)
		return
	}

	if !apiKeyPattern.MatchString(apiKey) {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("api_key"),
			"API key format looks unusual",
			"Expected `ccp_live_<token>` or `cl_test_<token>`. Continuing — but if you see 401 errors check the key.",
		)
	}

	c := client.New(endpoint, apiKey, p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *ccpProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		sshkey.New,
		vpc.New,
		vnet.New,
		containerinstance.New,
		blockvolume.New,
		publicip.New,
		objectbucket.New,
		vminstance.New,
		loadbalancer.New,
		containerscaleset.New,
		vmscaleset.New,
		k8scluster.New,
		k8snodepool.New,
		dbpginstance.New,
		dbvalkeyinstance.New,
		dbmysqlinstance.New,
		dbferretdbinstance.New,
		organization.New,
		apikey.New,
		orgmember.New,
		vnetpeering.New,
		vpcpeering.New,
		supportticket.New,
		ipaaspool.New,
		quotarequest.New,
		objectstoragekey.New,
		containersnapshot.New,
		vmsnapshot.New,
		vnetipresv.New,
		vnetfirewallrule.New,
	}
}

func (p *ccpProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		regions.New,
		organizations.New,
	}
}
