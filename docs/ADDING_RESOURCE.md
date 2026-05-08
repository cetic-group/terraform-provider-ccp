# Adding a new Terraform resource

This guide explains, step-by-step, how to add a new Cloud Lake resource to the
Terraform provider. The process is mechanical — most of the time is spent on
the API client + struct mapping, not on Terraform internals.

## Pattern reference

Look at `internal/resources/sshkey/sshkey.go` for the simplest possible
resource (no Update, just Create/Read/Delete + ImportState). For a richer
example with Update + nested objects, see `internal/resources/objectbucket/`.

## Step-by-step (estimated 2-4h per resource)

### 1. API client typed methods (`internal/client/`)

In `types.go`, add:

```go
// MyResource mirrors the API response.
type MyResource struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Region    string    `json:"region"`
    Status    string    `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    // ... add all fields the schema exposes
}

type MyResourceCreateRequest struct {
    Name   string `json:"name"`
    Region string `json:"region"`
    // ... required fields
}

type MyResourceUpdateRequest struct {
    Name *string `json:"name,omitempty"`  // pointer = optional
    // ... mutable fields only
}
```

In `client.go`, add the CRUD methods at the bottom (before `Poll`):

```go
// ─── My resource ─────────────────────────────────────────────────────────────

func (c *Client) ListMyResources(ctx context.Context, region string) ([]MyResource, error) {
    path := "/v1/my-resources"
    if region != "" {
        path += "?region=" + region
    }
    var out []MyResource
    if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
        return nil, err
    }
    return out, nil
}

func (c *Client) GetMyResource(ctx context.Context, id string) (*MyResource, error) {
    var out MyResource
    if err := c.do(ctx, http.MethodGet, "/v1/my-resources/"+id, nil, &out); err != nil {
        return nil, err
    }
    return &out, nil
}

func (c *Client) CreateMyResource(ctx context.Context, req MyResourceCreateRequest) (*MyResource, error) {
    var out MyResource
    if err := c.do(ctx, http.MethodPost, "/v1/my-resources", req, &out); err != nil {
        return nil, err
    }
    return &out, nil
}

func (c *Client) UpdateMyResource(ctx context.Context, id string, req MyResourceUpdateRequest) (*MyResource, error) {
    var out MyResource
    if err := c.do(ctx, http.MethodPatch, "/v1/my-resources/"+id, req, &out); err != nil {
        return nil, err
    }
    return &out, nil
}

func (c *Client) DeleteMyResource(ctx context.Context, id string) error {
    return c.do(ctx, http.MethodDelete, "/v1/my-resources/"+id, nil, nil)
}
```

### 2. Resource implementation (`internal/resources/myresource/myresource.go`)

Copy the template at the bottom of this doc and adapt:

- `myResourceModel` struct must match the schema 1:1 (tag names == attribute keys)
- `Schema` defines validators + plan modifiers (RequiresReplace for immutable
  fields, UseStateForUnknown for computed-stable fields)
- `Create` → API call + populate plan + State.Set
- `Read` → re-fetch + handle 404 via `client.IsNotFound`
- `Update` → only mutable fields
- `Delete` → handle "already gone" gracefully
- `ImportState` → `ImportStatePassthroughID` is enough for most cases

### 3. Wire into `provider.go`

```go
import (
    // ...
    "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/resources/myresource"
)

func (p *ccpProvider) Resources(_ context.Context) []func() resource.Resource {
    return []func() resource.Resource{
        // ... existing
        myresource.New,
    }
}
```

### 4. Example for users

Create `examples/myresource/main.tf`:

```hcl
terraform {
  required_providers {
    ccp = {
      source = "cetic-group/cetic-cloud-platform"
    }
  }
}

resource "ccp_my_resource" "example" {
  name   = "demo"
  region = "RNN"
}
```

### 5. Build + test

```bash
cd infrastructure/terraform
go build -o terraform-provider-cetic-cloud-platform .
make install  # if Makefile target exists
cd examples/myresource
terraform init
terraform plan
terraform apply
```

## Resource template (copy-paste)

Save as `internal/resources/myresource/myresource.go` and find/replace
`MyResource` / `myresource` / `my_resource` / `my-resources` to your case:

```go
// Package myresource implements the ccp_my_resource Terraform resource.
package myresource

import (
    "context"
    "fmt"

    "github.com/cetic-group/terraform-provider-cetic-cloud-platform/internal/client"
    "github.com/hashicorp/terraform-plugin-framework/path"
    "github.com/hashicorp/terraform-plugin-framework/resource"
    "github.com/hashicorp/terraform-plugin-framework/resource/schema"
    "github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
    "github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
    "github.com/hashicorp/terraform-plugin-framework/types"
)

var (
    _ resource.Resource                = (*myResourceResource)(nil)
    _ resource.ResourceWithConfigure   = (*myResourceResource)(nil)
    _ resource.ResourceWithImportState = (*myResourceResource)(nil)
)

func New() resource.Resource { return &myResourceResource{} }

type myResourceResource struct {
    client *client.Client
}

type myResourceResourceModel struct {
    ID        types.String `tfsdk:"id"`
    Name      types.String `tfsdk:"name"`
    Region    types.String `tfsdk:"region"`
    Status    types.String `tfsdk:"status"`
    CreatedAt types.String `tfsdk:"created_at"`
}

func (r *myResourceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.TypeName = req.ProviderTypeName + "_my_resource"
}

func (r *myResourceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
    resp.Schema = schema.Schema{
        MarkdownDescription: "Manages a Cloud Lake MyResource.",
        Attributes: map[string]schema.Attribute{
            "id": schema.StringAttribute{
                Computed: true,
                PlanModifiers: []planmodifier.String{
                    stringplanmodifier.UseStateForUnknown(),
                },
            },
            "name": schema.StringAttribute{
                Required: true,
            },
            "region": schema.StringAttribute{
                Required: true,
                PlanModifiers: []planmodifier.String{
                    stringplanmodifier.RequiresReplace(),
                },
            },
            "status": schema.StringAttribute{
                Computed: true,
                PlanModifiers: []planmodifier.String{
                    stringplanmodifier.UseStateForUnknown(),
                },
            },
            "created_at": schema.StringAttribute{
                Computed: true,
                PlanModifiers: []planmodifier.String{
                    stringplanmodifier.UseStateForUnknown(),
                },
            },
        },
    }
}

func (r *myResourceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    c, ok := req.ProviderData.(*client.Client)
    if !ok {
        resp.Diagnostics.AddError("Unexpected provider data type",
            fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
        return
    }
    r.client = c
}

func (r *myResourceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
    var plan myResourceResourceModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
    if resp.Diagnostics.HasError() {
        return
    }
    created, err := r.client.CreateMyResource(ctx, client.MyResourceCreateRequest{
        Name:   plan.Name.ValueString(),
        Region: plan.Region.ValueString(),
    })
    if err != nil {
        resp.Diagnostics.AddError("Failed to create MyResource", err.Error())
        return
    }
    plan.ID = types.StringValue(created.ID)
    plan.Status = types.StringValue(created.Status)
    plan.CreatedAt = types.StringValue(created.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
    resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *myResourceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
    var state myResourceResourceModel
    resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
    if resp.Diagnostics.HasError() {
        return
    }
    got, err := r.client.GetMyResource(ctx, state.ID.ValueString())
    if err != nil {
        if client.IsNotFound(err) {
            resp.State.RemoveResource(ctx)
            return
        }
        resp.Diagnostics.AddError("Failed to read MyResource", err.Error())
        return
    }
    state.Name = types.StringValue(got.Name)
    state.Status = types.StringValue(got.Status)
    state.CreatedAt = types.StringValue(got.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
    resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *myResourceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
    var plan myResourceResourceModel
    resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
    if resp.Diagnostics.HasError() {
        return
    }
    name := plan.Name.ValueString()
    updated, err := r.client.UpdateMyResource(ctx, plan.ID.ValueString(), client.MyResourceUpdateRequest{
        Name: &name,
    })
    if err != nil {
        resp.Diagnostics.AddError("Failed to update MyResource", err.Error())
        return
    }
    plan.Name = types.StringValue(updated.Name)
    plan.Status = types.StringValue(updated.Status)
    resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *myResourceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
    var state myResourceResourceModel
    resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
    if resp.Diagnostics.HasError() {
        return
    }
    if err := r.client.DeleteMyResource(ctx, state.ID.ValueString()); err != nil {
        if client.IsNotFound(err) {
            return
        }
        resp.Diagnostics.AddError("Failed to delete MyResource", err.Error())
        return
    }
}

func (r *myResourceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
    resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
```

## Backlog des resources à ajouter (Phase 8)

| Priorité | Resource | Endpoint API | Notes |
|----------|----------|--------------|-------|
| Haute | `ccp_load_balancer` | `/v1/load-balancers` | Avec sub-blocks listeners + backends |
| Haute | `ccp_container_scale_set` | `/v1/container-scale-sets` | replicas hot-mutable |
| Haute | `ccp_vm_scale_set` | `/v1/vm-scale-sets` | idem |
| Moyenne | `ccp_db_pg_instance` | `/v1/db/pg` | + variantes mysql/redis/mongo |
| Moyenne | `ccp_k8s_cluster` | `/v1/k8s/clusters` | + sub `ccp_k8s_node_pool` |
| Moyenne | `ccp_vnet_peering` | `/v1/vnet-peerings` | |
| Basse | `ccp_organization` | `/v1/orgs` | |
| Basse | `ccp_api_key` | `/v1/api-keys` | Token retourné une seule fois — gérer côté state |
| Basse | `ccp_org_member` | `/v1/members` | |
| Basse | `ccp_support_ticket` | `/v1/support/tickets` | Pas vraiment IaC use-case |
| Basse | `ccp_ipaas_pool` | admin only | |
| Basse | `ccp_quota_request` | `/v1/quotas/requests` | One-shot |
| Basse | `ccp_tag` | `/v1/tags` | Existe déjà comme attribut sur les autres resources |

Estimation : ~2-4h par resource. Total ~30-50h pour les 14.
