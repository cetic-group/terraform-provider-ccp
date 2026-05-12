# terraform-provider-cetic-cloud-platform — CLAUDE.md

> Provider Terraform officiel pour CETIC Cloud Platform.
> Source de vérité backend : `cetic-cloud-platform/apps/api/`.
> Repo public : https://github.com/cetic-group/terraform-provider-cetic-cloud-platform
> Terraform Registry : `cetic-group/cetic-cloud-platform`

---

## Convention release — **ALIGNER DOC + README À CHAQUE TAG**

À chaque release du provider (`vX.Y.Z`), **mettre à jour systématiquement** :

1. **`docs/index.md`** — exemples du provider doivent référencer la nouvelle version :
   ```hcl
   required_providers {
     ccp = {
       source  = "cetic-group/cetic-cloud-platform"
       version = "~> X.Y.Z"   # ← bump à la version qu'on tag
     }
   }
   ```
   Mettre à jour **toutes** les occurrences (il y en a typiquement 2-3 dans le doc).

2. **`README.md`** — exemples + badges + section "Installation" : aligner sur `~> X.Y` (compat majeure) ou `>= X.Y.Z` (lock à la min).

3. **`docs/resources/*.md`** et **`docs/data-sources/*.md`** — si une resource/datasource a été ajoutée ou modifiée dans cette release, sa doc doit refléter le schema final. Particulièrement vérifier :
   - Les attributs Required vs Optional vs Computed sont fidèles au code Go (`internal/resources/<r>/<r>.go::Schema()`)
   - Les exemples HCL utilisent les bons noms de champs (pas `label` quand le code dit `name`, etc.)
   - Les "Notes" mentionnent toute breaking change

4. **`internal/provider/version.go`** (si présent) — le provider expose sa version au Registry via ce constant.

5. **`.goreleaser.yml`** — pas de modif systématique mais vérifier que les binaires sont bien build pour les 6 plateformes (linux/macos/windows × amd64/arm64).

### Workflow git pour tagger une release

```bash
# 1. Sur main, après un merge de PR (fix/feat) :
git pull origin main
git status   # doit être clean

# 2. Bump doc/README pour la version cible (commit séparé)
sed -i 's|version = "~> 0\.7\.0"|version = "~> 0.8.0"|g' docs/index.md README.md  # exemple v0.8.0
git add docs/index.md README.md docs/resources/*.md
git -c user.email="<email>" commit -m "docs: bump provider version references to v0.8.0"

# 3. Push + tag annotated
git push origin main
git tag -a v0.8.0 -m "v0.8.0 — <résumé des changements>"
git push origin v0.8.0

# 4. goreleaser auto via GitHub Actions (.github/workflows/release.yml)
gh run list --limit 3
gh run watch  # attends que goreleaser finisse (~4 min)

# 5. Vérifier sur Terraform Registry (latency ~5 min)
#    https://registry.terraform.io/providers/cetic-group/cetic-cloud-platform/latest
```

### Versionnage SemVer

- **Major** (`v1.0.0`) : breaking change dans le schema (rename de champ existant, suppression).
- **Minor** (`v0.8.0`) : nouveau champ Optional, nouvelle resource, nouveau datasource. Backward-compatible.
- **Patch** (`v0.8.1`) : bug fix, doc fix, internal refactor.

### Modules consommateurs à bumper aussi

Après release du provider, **mettre à jour** dans `cetic-cloud-terraform-modules` :
- Tous les `versions.tf` des modules / landing-zones / examples : `version = ">= X.Y.Z"`.
- README.md du repo modules (badges + exemple Quick Start).

Idéalement faire une PR sur `cetic-cloud-terraform-modules` juste après le release du provider, dans la même fenêtre temporelle.

---

## Stack provider

Go 1.22+ · `terraform-plugin-framework` (pas le legacy SDK v2) · `proxmoxer` non utilisé (le provider parle au backend CCP via REST sur `apps/api`).

## Layout

```
internal/
  client/                    # Client HTTP vers l'API CCP
  datasources/               # Datasources (1 dossier par)
    dbengineversions/
    dbplans/
    k8stemplates/
    lxctemplates/
    organizations/
    qemutemplates/
    regions/
  provider/
    provider.go              # Schema provider + DataSources() + Resources()
  resources/                 # Resources (1 dossier par)
    apikey/
    blockvolume/
    containerinstance/
    containerscaleset/
    customtemplate/
    dbferretdbinstance/ ...
    ipaaspool/
    k8scluster/ ...
    loadbalancer/
    objectbucket/
    objectstoragekey/
    organization/
    orgmember/
    publicip/
    quotarequest/
    sshkey/
    supportticket/
    vminstance/
    vmscaleset/
    vmsnapshot/
    vnet/
    vnetfirewallrule/
    vnetipresv/
    vpc/
    vpcpeering/
docs/
  index.md                   # Provider config + getting started
  resources/                 # 1 fichier .md par resource
  data-sources/              # 1 fichier .md par datasource
examples/                    # Exemples auto-testés (acceptance tests)
```

## Conventions code

- 1 resource = 1 dossier `internal/resources/<r>/<r>.go` avec :
  - `<r>Resource` struct (le `client *client.Client`)
  - `<r>Model` struct (les `tfsdk:"..."` tags)
  - `Metadata()`, `Schema()`, `Configure()`, `Create()`, `Read()`, `Update()`, `Delete()`, `ImportState()`
  - Helper `stateFrom(api *client.<R>) <r>Model` pour le mapping API → state
- 1 datasource = 1 dossier `internal/datasources/<d>/<d>.go` similaire.
- Toujours enregistrer dans `internal/provider/provider.go` :
  - `Resources()` retourne tous les `func() resource.Resource { return &<r>Resource{} }`
  - `DataSources()` idem.
- Validators : utiliser `validator.String/Int64/...` du framework (`int64validator.Between(...)`, `stringvalidator.RegexMatches(...)`).
- PlanModifiers : `stringplanmodifier.RequiresReplace()` pour les champs immutables, `UseStateForUnknown()` pour les Computed stables.

## Conventions doc

- Frontmatter YAML obligatoire (`page_title`, `subcategory`, `description`).
- Structure systématique : `Example Usage` → `Argument Reference` (Required puis Optional) → `Attributes Reference` (Computed) → `Import`.
- Les exemples HCL doivent compiler sans modif (sont vérifiés par `tflint` / `terraform validate` côté repo modules).
- Les noms de champs dans la doc doivent matcher exactement les `tfsdk:"..."` du code Go. Tout désalignement est un bug.

## Pièges Terraform plugin framework — vécus en prod

Trois erreurs récurrentes à connaître quand on écrit/modifie un `Create()` ou un `ModifyPlan()` :

### 1. "Provider produced inconsistent result after apply"

Cause typique : on écrase `plan.<RequiredField>` avec la valeur retournée par l'API à la fin de `Create()`, et cette valeur diffère de ce que l'utilisateur a écrit en HCL (canonicalisation, normalisation, swap, etc.).

→ **Pour les attributs `Required`, ne jamais réécrire le plan avec une valeur backend qui peut différer du config.** Appliquer la transformation au moment de construire la requête API, pas au moment d'écrire le state. Préserver l'ordre/format de l'utilisateur.

Exemple historique : `ccp_vnet_peering` v0.9.0 — `canonicalOrder(a, b)` était appliqué au state, le state stockait `(min, max)`, mais le plan avait `(a, b)` user → mismatch → erreur. Fix v0.9.3 : envoyer la canonical à l'API mais laisser plan/state intacts.

### 2. "Provider produced invalid plan"

Cause typique : un `ModifyPlan()` modifie la valeur planifiée d'un attribut **`Required`**, donc la planned value diffère du config value.

→ **`ModifyPlan` ne peut pas changer la valeur d'un attribut `Required`.** Il peut :
- Changer la valeur d'un attribut `Computed` ou `Optional+Computed`.
- Ajouter à `resp.RequiresReplace` pour forcer un destroy+create.
- Émettre des diagnostics.

Si tu veux normaliser un input utilisateur, fais-le côté client API (Create) en gardant le plan inchangé, OU bien valide-le strictement via un `Validator` au lieu de réécrire.

Exemple historique : `ccp_vnet_peering` v0.9.1 — `ModifyPlan` swappait les UUIDs en canonical ; Terraform a refusé. Reverté en v0.9.3.

### 3. `applyXToModel(api, &plan)` écrase l'intent utilisateur

Plusieurs resources ont un helper qui mappe la struct API vers le model Terraform en écrasant tous les champs (`dst.Foo = types.BoolValue(src.Foo)` etc.). Si tu appelles ce helper **avant** de relire l'intent utilisateur sur un champ Optional+Computed (ex. `isolated`, `enabled`), le plan se retrouve avec la valeur backend (souvent `false` juste après création) et la logique conditionnelle qui suit teste contre la valeur écrasée → silent skip.

→ **Capturer l'intent utilisateur dans une variable locale AVANT d'appeler `applyXToModel`.** Exemple :

```go
wantIsolated := !plan.Isolated.IsNull() && !plan.Isolated.IsUnknown() && plan.Isolated.ValueBool()

diags = applyVNetToModel(ctx, final, &plan)
// plan.Isolated est maintenant écrasée à false (valeur API)

if wantIsolated && !final.Isolated {
    r.client.SetVNetIsolation(ctx, final.ID, true)
    plan.Isolated = types.BoolValue(true) // remettre la valeur user pour le state
}
```

Exemple historique : `ccp_vnet` Create avec `isolated = true` → état final `isolated = false`, "inconsistent result". Fix v0.9.2.

### 4. `ValidateConfig` fire au `terraform validate` sur des Optional+Computed+Default

Cause typique : un `ValidateConfig` checke `!cfg.X.IsNull() && !cfg.Y.IsNull() && !xSet && !ySet` pour catcher "les 2 explicitement false". Au `terraform validate`, **avant que les PlanModifiers ne s'exécutent**, les attributs `Optional+Computed` avec `booldefault.StaticBool(...)` sont **Unknown**, pas Null. La condition gating considère donc Unknown comme une valeur concrète, calcule `xSet = false` et `ySet = false`, et déclenche l'erreur — alors qu'aucun consumer ne pourrait raisonnablement écrire `false` partout en plan-time (les defaults `true` s'appliqueront).

→ **Dans `ValidateConfig`, early-return si EITHER attribut est Null OU Unknown.** Le plan-time enforcement (resource logic + API CHECK constraint) reste actif sur les valeurs concrètes.

```go
func (r *xResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
    var cfg xResourceModel
    resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
    if resp.Diagnostics.HasError() { return }
    // Skip when either side is unresolved — defaults may not have been applied yet.
    if cfg.A.IsNull() || cfg.A.IsUnknown() || cfg.B.IsNull() || cfg.B.IsUnknown() {
        return
    }
    if !cfg.A.ValueBool() && !cfg.B.ValueBool() {
        resp.Diagnostics.AddError("invariant violated", "...")
    }
}
```

Exemple historique : `ccp_registry.ValidateConfig` v0.11.0 → tout `make validate` sur `cetic-cloud-terraform-modules` cassait pour les consumers qui n'explicitaient pas `expose_public`/`expose_private`. Fix v0.11.1.

## Mots réservés Terraform

Ne **jamais** utiliser comme nom d'attribut :
- `count`, `for_each`, `provider`, `lifecycle`, `depends_on`, `dynamic`, `module`, `output`, `variable`, `locals`, `data`, `resource`, `terraform`

Sinon le schema crash au load avec `Reserved Root Attribute/Block Name`. Préférer un préfixe ou suffixe explicite (`ip_count` au lieu de `count`, `node_provider` au lieu de `provider`).

## Tests

- Unit : `go test ./internal/...`
- Acceptance : `TF_ACC=1 go test ./internal/resources/...` (nécessite un endpoint CCP réel + `CCP_API_KEY`).
- Compatibilité avec modules consommateurs : utiliser `dev_overrides` (cf. `cetic-cloud-terraform-modules/CLAUDE.md`).

## Build local

```bash
go build -o ./terraform-provider-cetic-cloud-platform .
```

Le binaire est ensuite picked up par les modules consommateurs si leur `~/.terraformrc` a un `dev_overrides` qui pointe vers ce dossier.
