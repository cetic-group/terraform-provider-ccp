// Package iampolicydocument implements the ccp_iam_policy_document data source.
//
// This is a pure HCL → JSON transformation — no API call. It mirrors the
// ergonomic interface popularised by `aws_iam_policy_document`:
//
//	data "ccp_iam_policy_document" "this" {
//	  statement {
//	    sid     = "AllowRegistryAdmin"
//	    effect  = "Allow"
//	    actions = ["registry:*"]
//	    resources = ["arn:ccp:registry:rnn:T:registry/myreg*"]
//	    condition {
//	      test     = "IpAddress"
//	      variable = "SourceIp"
//	      values   = ["203.0.113.0/24"]
//	    }
//	  }
//	}
//
// The `json` output attribute is a stable, JCS-style canonicalised JSON
// string (UTF-8, sorted keys, no extra whitespace) — bit-for-bit
// reproducible across runs given the same input. It can be plugged into
// `ccp_iam_role.policy_document_json` without further processing; the
// API will re-canonicalise but the SHA-256 hash will be identical.
package iampolicydocument

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource = (*policyDocumentDataSource)(nil)
)

// New returns the data source factory used by `provider.DataSources()`.
func New() datasource.DataSource { return &policyDocumentDataSource{} }

type policyDocumentDataSource struct{}

// policyDocumentModel is the top-level data source state.
type policyDocumentModel struct {
	Version    types.String          `tfsdk:"version"`
	JSON       types.String          `tfsdk:"json"`
	JSONSha256 types.String          `tfsdk:"json_sha256"`
	Statement  []policyStatementBlock `tfsdk:"statement"`
}

// policyStatementBlock mirrors `statement {}` in HCL.
type policyStatementBlock struct {
	SID       types.String          `tfsdk:"sid"`
	Effect    types.String          `tfsdk:"effect"`
	Actions   []types.String        `tfsdk:"actions"`
	Resources []types.String        `tfsdk:"resources"`
	Condition []policyConditionBlock `tfsdk:"condition"`
}

// policyConditionBlock mirrors `condition {}` (repeatable inside a statement).
type policyConditionBlock struct {
	Test     types.String   `tfsdk:"test"`
	Variable types.String   `tfsdk:"variable"`
	Values   []types.String `tfsdk:"values"`
}

func (d *policyDocumentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_policy_document"
}

func (d *policyDocumentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Renders a CETIC Cloud IAM PolicyDocument (Roles v1) from ergonomic HCL " +
			"blocks. The output `json` attribute is a canonical JSON string suitable for " +
			"`ccp_iam_role.policy_document_json` — stable bit-for-bit across runs, sorted keys, no " +
			"extraneous whitespace.\n\n" +
			"This is a **pure local transformation** — no API call is performed. v1 condition " +
			"operators: `StringEquals`, `StringNotEquals`, `StringLike`, `IpAddress`, `NotIpAddress`, " +
			"`DateGreaterThan`, `DateLessThan`. v1 condition keys: `SourceIp`, `RequestTime`, " +
			"`RequestRegion`, `ResourceTag`, `RequestTag`, `OrgId`, `ApiKeyPrefix`, `PrincipalType`.",
		Attributes: map[string]schema.Attribute{
			"version": schema.StringAttribute{
				MarkdownDescription: "Policy version. Defaults to `2026-05-10` (the only currently " +
					"supported value).",
				Optional: true,
				Computed: true,
			},
			"json": schema.StringAttribute{
				MarkdownDescription: "Rendered policy document as a canonical JSON string.",
				Computed:            true,
			},
			"json_sha256": schema.StringAttribute{
				MarkdownDescription: "Hex SHA-256 of the canonical JSON — useful as a stable identifier " +
					"for the policy content (e.g. as a Terraform key).",
				Computed: true,
			},
		},
		Blocks: map[string]schema.Block{
			"statement": schema.ListNestedBlock{
				MarkdownDescription: "One or more policy statements. At least one is required.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"sid": schema.StringAttribute{
							MarkdownDescription: "Optional statement identifier (free-form short label).",
							Optional:            true,
						},
						"effect": schema.StringAttribute{
							MarkdownDescription: "`Allow` or `Deny`. Defaults to `Allow` when omitted.",
							Optional:            true,
							Computed:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("Allow", "Deny"),
							},
						},
						"actions": schema.ListAttribute{
							MarkdownDescription: "List of action strings, e.g. `registry:Push`. Wildcards " +
								"allowed (`registry:*`, `*:Get*`).",
							ElementType: types.StringType,
							Required:    true,
						},
						"resources": schema.ListAttribute{
							MarkdownDescription: "List of resource ARN patterns (cf. iam-arn-scheme). " +
								"`*` allowed as a wildcard.",
							ElementType: types.StringType,
							Required:    true,
						},
					},
					Blocks: map[string]schema.Block{
						"condition": schema.ListNestedBlock{
							MarkdownDescription: "Optional condition clause(s). Each condition is a " +
								"(test, variable, values) tuple; multiple conditions inside a " +
								"statement are AND-combined.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"test": schema.StringAttribute{
										MarkdownDescription: "Condition operator (e.g. `StringEquals`, " +
											"`IpAddress`, `DateGreaterThan`).",
										Required: true,
									},
									"variable": schema.StringAttribute{
										MarkdownDescription: "Condition key (e.g. `SourceIp`, `RequestTime`, " +
											"`OrgId`).",
										Required: true,
									},
									"values": schema.ListAttribute{
										MarkdownDescription: "List of values for the condition. Multi-value " +
											"is OR-combined inside the operator.",
										ElementType: types.StringType,
										Required:    true,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// Configure is a no-op — the data source does not need the HTTP client.
func (d *policyDocumentDataSource) Configure(_ context.Context, _ datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
}

func (d *policyDocumentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg policyDocumentModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	version := client.PolicyVersion20260510
	if !cfg.Version.IsNull() && !cfg.Version.IsUnknown() && cfg.Version.ValueString() != "" {
		version = cfg.Version.ValueString()
	}

	if len(cfg.Statement) == 0 {
		resp.Diagnostics.AddError("Missing statement block",
			"At least one `statement {}` block is required.")
		return
	}

	statements := make([]map[string]any, 0, len(cfg.Statement))
	for i, s := range cfg.Statement {
		stmt := map[string]any{}

		if !s.SID.IsNull() && !s.SID.IsUnknown() && s.SID.ValueString() != "" {
			stmt["sid"] = s.SID.ValueString()
		}

		effect := "Allow"
		if !s.Effect.IsNull() && !s.Effect.IsUnknown() && s.Effect.ValueString() != "" {
			effect = s.Effect.ValueString()
		}
		stmt["effect"] = effect

		actions := make([]string, 0, len(s.Actions))
		for _, a := range s.Actions {
			actions = append(actions, a.ValueString())
		}
		if len(actions) == 0 {
			resp.Diagnostics.AddError(
				fmt.Sprintf("statement[%d] has empty actions list", i),
				"Each statement must declare at least one action.")
			return
		}
		stmt["actions"] = actions

		resources := make([]string, 0, len(s.Resources))
		for _, r := range s.Resources {
			resources = append(resources, r.ValueString())
		}
		if len(resources) == 0 {
			resp.Diagnostics.AddError(
				fmt.Sprintf("statement[%d] has empty resources list", i),
				"Each statement must declare at least one resource ARN.")
			return
		}
		stmt["resources"] = resources

		if len(s.Condition) > 0 {
			// Compose `{ <test>: { <variable>: [<values>] } }`. Multiple
			// conditions on the same (test, variable) merge their values.
			conds := map[string]map[string][]string{}
			for _, c := range s.Condition {
				test := c.Test.ValueString()
				variable := c.Variable.ValueString()
				vals := make([]string, 0, len(c.Values))
				for _, v := range c.Values {
					vals = append(vals, v.ValueString())
				}
				if conds[test] == nil {
					conds[test] = map[string][]string{}
				}
				conds[test][variable] = append(conds[test][variable], vals...)
			}
			stmt["condition"] = conds
		}

		statements = append(statements, stmt)
	}

	doc := map[string]any{
		"version":    version,
		"statements": statements,
	}

	canonical, err := canonicalizeJSON(doc)
	if err != nil {
		resp.Diagnostics.AddError("Failed to render policy document", err.Error())
		return
	}

	sum := sha256.Sum256([]byte(canonical))

	state := policyDocumentModel{
		Version:    types.StringValue(version),
		JSON:       types.StringValue(canonical),
		JSONSha256: types.StringValue(hex.EncodeToString(sum[:])),
		Statement:  cfg.Statement,
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// canonicalizeJSON serialises `v` as a compact, deterministic JSON string:
// keys sorted lexicographically at every level of nested objects, no
// extra whitespace, UTF-8. Equivalent in spirit to JCS RFC 8785 with the
// simplifications appropriate for the limited set of types used in
// PolicyDocument (string, list[string], nested maps).
func canonicalizeJSON(v any) (string, error) {
	// First marshal-unmarshal to normalise to plain Go types
	// (map[string]any / []any / string).
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var normalised any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&normalised); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, normalised); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeCanonical(w *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			w.Write(kb)
			w.WriteByte(':')
			if err := writeCanonical(w, x[k]); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	case []any:
		w.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := writeCanonical(w, e); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		w.Write(b)
	}
	return nil
}
