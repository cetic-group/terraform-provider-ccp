package provider

import (
	"context"
	"os"
	"regexp"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	dsapikey "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/apikey"
	dsapplicationgateway "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/applicationgateway"
	dsblockvolume "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/blockvolume"
	dscontainerinstance "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/containerinstance"
	dscontainerscaleset "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/containerscaleset"
	dscontainersnapshot "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/containersnapshot"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/containertemplates"
	dscustomtemplate "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/customtemplate"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbcredentials"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbengineversions"
	dsdbferretdbinstance "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbferretdbinstance"
	dsdbmysqlinstance "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbmysqlinstance"
	dsdbpginstance "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbpginstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbplans"
	dsdbvalkeyinstance "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/dbvalkeyinstance"
	dsiampolicydocument "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/iampolicydocument"
	dsiamrole "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/iamrole"
	dsipaaspool "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/ipaaspool"
	dsk8scluster "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/k8scluster"
	dsk8snodepool "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/k8snodepool"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/k8stemplates"
	dsloadbalancer "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/loadbalancer"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/lxctemplates"
	dsobjectbucket "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/objectbucket"
	dsobjectstoragekey "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/objectstoragekey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/organizations"
	dspricing "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/pricing"
	dspromocodes "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/promocodes"
	dspublicip "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/publicip"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/qemutemplates"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/regions"
	dsregistry "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/registry"
	dsregistryacl "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/registryacl"
	dsregistryuser "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/registryuser"
	dssecret "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/secret"
	dsserviceaccount "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/serviceaccount"
	dssshkey "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/sshkey"
	dssupportplan "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/supportplan"
	dsvminstance "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vminstance"
	dsvmscaleset "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vmscaleset"
	dsvmsnapshot "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vmsnapshot"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vmtemplates"
	dsvnet "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vnet"
	dsvnetfirewallrule "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vnetfirewallrule"
	dsvnetipresv "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vnetipresv"
	dsvnetpeering "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vnetpeering"
	dsvpc "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/datasources/vpc"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/apikey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/appgwlistener"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/appgwroute"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/appgwtargetgroup"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/appgwtargetgroupmember"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/applicationgateway"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/blockvolume"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/budget"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/commit"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/containerinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/containerscaleset"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/containersnapshot"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/customtemplate"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbferretdbinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbmysqlinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbpginstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/dbvalkeyinstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/iamrole"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/iamroleassignment"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/ipaaspool"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/k8scluster"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/k8snodepool"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/loadbalancer"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/objectbucket"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/objectstoragekey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/organization"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/orgmember"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/publicip"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/quotarequest"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/registry"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/registryacl"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/registryuser"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/secret"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/serviceaccount"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/sshkey"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/supportsubscription"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/supportticket"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vminstance"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vmscaleset"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vmsnapshot"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnet"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnetfirewallrule"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnetipresv"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vnetpeering"
	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/vpc"
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
	resp.TypeName = "cetic-cloud-platform"
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

var apiKeyPattern = regexp.MustCompile(`^ccp_(live|test)_[A-Za-z0-9_-]{20,}$`)

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
			"Expected `ccp_live_<token>` or `ccp_test_<token>`. Continuing — but if you see 401 errors check the key.",
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
		applicationgateway.New,
		appgwlistener.New,
		appgwtargetgroup.New,
		appgwtargetgroupmember.New,
		appgwroute.New,
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
		supportticket.New,
		supportsubscription.New,
		ipaaspool.New,
		quotarequest.New,
		objectstoragekey.New,
		containersnapshot.New,
		vmsnapshot.New,
		vnetipresv.New,
		vnetfirewallrule.New,
		customtemplate.New,
		registry.New,
		registryuser.New,
		registryacl.New,
		iamrole.New,
		iamroleassignment.New,
		serviceaccount.New,
		secret.New,
		budget.New,
		commit.New,
	}
}

func (p *ccpProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		// Catalogs / static
		regions.New,
		organizations.New,
		containertemplates.New,
		vmtemplates.New,
		// Deprecated aliases kept for backward compat — removed in v2.0.0.
		// Use ccp_container_templates and ccp_vm_templates instead.
		lxctemplates.New,
		qemutemplates.New,
		dbplans.New,
		dbengineversions.New,
		k8stemplates.New,
		dspricing.New,
		dspromocodes.New,
		dssupportplan.New,
		// Network
		dsvpc.New,
		dsvnet.New,
		dsvnetpeering.New,
		dsvnetfirewallrule.New,
		dsvnetipresv.New,
		dspublicip.New,
		dsloadbalancer.New,
		dsipaaspool.New,
		dsapplicationgateway.New,
		// Compute
		dsvminstance.New,
		dsvmscaleset.New,
		dsvmsnapshot.New,
		dscontainerinstance.New,
		dscontainerscaleset.New,
		dscontainersnapshot.New,
		dscustomtemplate.New,
		// Storage
		dsblockvolume.New,
		dsobjectbucket.New,
		dsobjectstoragekey.New,
		// Kubernetes
		dsk8scluster.New,
		dsk8snodepool.New,
		// Database
		dsdbpginstance.New,
		dsdbmysqlinstance.New,
		dsdbvalkeyinstance.New,
		dsdbferretdbinstance.New,
		dbcredentials.NewPG,
		dbcredentials.NewMySQL,
		dbcredentials.NewFerretdb,
		dbcredentials.NewValkey,
		// Container Registry
		dsregistry.New,
		dsregistryuser.New,
		dsregistryacl.New,
		// IAM / Identity
		dsiamrole.New,
		dsiampolicydocument.New,
		dsserviceaccount.New,
		dssshkey.New,
		dsapikey.New,
		// Secrets
		dssecret.New,
	}
}
