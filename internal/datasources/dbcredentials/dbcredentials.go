// Package dbcredentials implements 4 read-only data sources to fetch the
// admin credentials for managed database instances :
//
//	ccp_db_pg_credentials       → /v1/db/pg/{id}/credentials
//	ccp_db_mysql_credentials    → /v1/db/mysql/{id}/credentials
//	ccp_db_ferretdb_credentials → /v1/db/ferretdb/{id}/credentials
//	ccp_db_valkey_credentials   → /v1/db/valkey/{id}/credentials
//
// PG / MySQL / FerretDB return {username, password, database, host, port, uri}.
// Valkey returns {password, host, port, uri} (no username, no database).
//
// Why a data source and not an attribute on the resource: avoids storing the
// password in the resource's primary state file when the user doesn't need it,
// and surfaces the password via an explicit, scoped lookup.
package dbcredentials

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// engine identifies which API path to hit + which fields are returned.
type engine int

const (
	enginePG engine = iota
	engineMySQL
	engineFerretdb
	engineValkey
)

func (e engine) typeName() string {
	switch e {
	case enginePG:
		return "_db_pg_credentials"
	case engineMySQL:
		return "_db_mysql_credentials"
	case engineFerretdb:
		return "_db_ferretdb_credentials"
	case engineValkey:
		return "_db_valkey_credentials"
	}
	return ""
}

func (e engine) apiPath(id string) string {
	switch e {
	case enginePG:
		return "/v1/db/pg/" + id + "/credentials"
	case engineMySQL:
		return "/v1/db/mysql/" + id + "/credentials"
	case engineFerretdb:
		return "/v1/db/ferretdb/" + id + "/credentials"
	case engineValkey:
		return "/v1/db/valkey/" + id + "/credentials"
	}
	return ""
}

func (e engine) hasUsernameAndDatabase() bool {
	return e == enginePG || e == engineMySQL || e == engineFerretdb
}

func (e engine) friendlyLabel() string {
	switch e {
	case enginePG:
		return "PostgreSQL"
	case engineMySQL:
		return "MySQL-compatible"
	case engineFerretdb:
		return "FerretDB v2 (MongoDB-compatible)"
	case engineValkey:
		return "Valkey (Redis-compatible)"
	}
	return ""
}

// ─── Public constructors — one per engine ─────────────────────────────────────

func NewPG() datasource.DataSource       { return &dbCredsDataSource{eng: enginePG} }
func NewMySQL() datasource.DataSource    { return &dbCredsDataSource{eng: engineMySQL} }
func NewFerretdb() datasource.DataSource { return &dbCredsDataSource{eng: engineFerretdb} }
func NewValkey() datasource.DataSource   { return &dbCredsDataSource{eng: engineValkey} }

// ─── Implementation ───────────────────────────────────────────────────────────

var (
	_ datasource.DataSource              = &dbCredsDataSource{}
	_ datasource.DataSourceWithConfigure = &dbCredsDataSource{}
)

type dbCredsDataSource struct {
	eng    engine
	client *client.Client
}

type credsModel struct {
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Database types.String `tfsdk:"database"`
	Host     types.String `tfsdk:"host"`
	Port     types.Int64  `tfsdk:"port"`
	URI      types.String `tfsdk:"uri"`
}

// API response — all fields optional so the same struct works for the 4
// engines (Valkey omits username + database).
type apiResponse struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	URI      string `json:"uri"`
}

func (d *dbCredsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "ccp" + d.eng.typeName()
}

func (d *dbCredsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "UUID of the " + d.eng.friendlyLabel() + " instance.",
			Required:            true,
		},
		"password": schema.StringAttribute{
			MarkdownDescription: "Admin password — sensitive, do not commit.",
			Computed:            true,
			Sensitive:           true,
		},
		"host": schema.StringAttribute{
			MarkdownDescription: "Endpoint host (private VNet IP).",
			Computed:            true,
		},
		"port": schema.Int64Attribute{
			MarkdownDescription: "Endpoint port.",
			Computed:            true,
		},
		"uri": schema.StringAttribute{
			MarkdownDescription: "Connection URI ready to plug into a client. **Sensitive** (contains the password).",
			Computed:            true,
			Sensitive:           true,
		},
	}
	if d.eng.hasUsernameAndDatabase() {
		attrs["username"] = schema.StringAttribute{
			MarkdownDescription: "Admin username.",
			Computed:            true,
		}
		attrs["database"] = schema.StringAttribute{
			MarkdownDescription: "Admin database name.",
			Computed:            true,
		}
	} else {
		// Schema requires every field of the model — expose as Computed
		// but the API doesn't fill them, so they will be the zero value.
		attrs["username"] = schema.StringAttribute{
			MarkdownDescription: "Always empty for this engine (no per-user authentication).",
			Computed:            true,
		}
		attrs["database"] = schema.StringAttribute{
			MarkdownDescription: "Always empty for this engine (no logical database concept).",
			Computed:            true,
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Fetches the admin credentials of a " + d.eng.friendlyLabel() +
			" managed instance. Use this to wire a secret into a downstream resource (e.g. a Kubernetes Secret) without committing the password to your tfvars file.",
		Attributes: attrs,
	}
}

func (d *dbCredsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *dbCredsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state credsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	if id == "" {
		resp.Diagnostics.AddError("Missing id", "The data source requires `id` (UUID of the instance).")
		return
	}

	var out apiResponse
	if err := d.client.DoRaw(ctx, http.MethodGet, d.eng.apiPath(id), nil, &out); err != nil {
		resp.Diagnostics.AddError(
			"Failed to fetch "+d.eng.friendlyLabel()+" credentials",
			fmt.Sprintf("API error for instance %s: %s", id, err.Error()),
		)
		return
	}

	state.Username = types.StringValue(out.Username)
	state.Password = types.StringValue(out.Password)
	state.Database = types.StringValue(out.Database)
	state.Host = types.StringValue(out.Host)
	state.Port = types.Int64Value(int64(out.Port))
	state.URI = types.StringValue(out.URI)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
