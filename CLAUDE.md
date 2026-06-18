# terraform-provider-ccp — CLAUDE.md

> Provider Terraform officiel pour CETIC Cloud Platform.
> Source de vérité backend : `cetic-cloud-platform/apps/api/`.
> Repo public : https://github.com/cetic-group/terraform-provider-ccp
> Terraform Registry : `cetic-group/ccp`

---

## Convention release — **TOUJOURS DEPUIS `main`** + **BUMP AVANT TAG**

⚠️ **Règle critique #0 — RELEASE DEPUIS `main` UNIQUEMENT, JAMAIS depuis une branche feature.**
Le `git tag vX.Y.Z` qui déclenche la release **doit** être posé sur `main`, **après** avoir mergé la PR (fix/feat). Tagger depuis une branche feature publie une version que `main` ne contient pas (drift : Registry/prod en avance sur main) ET embarque dans le tag du code non-mergé. Avant tout tag : `git checkout main && git pull origin main` puis vérifier que le commit visé est sur main.
Incident 2026-06-14 : `v4.10.0` + `v4.10.1` taggés depuis `feat/windows-instance` → main n'avait ni les fixes ni les releases ; rattrapé en mergeant la branche dans main a posteriori. **Ne plus jamais releaser hors de main.**

⚠️ **Règle critique #1** : avant `git tag` qui déclenche goreleaser et publie sur le Registry, **toujours** bumper les contraintes `version = "~> X.Y"` dans **TOUS les exemples HCL** du repo. Le Registry rend ces exemples verbatim — un tag avec exemples stale verrouille les copy-paste users sur l'ancienne version sans qu'ils s'en rendent compte. Voir mémoire `feedback-tf-provider-bump-version-examples-before-tag` (incident 2026-05-28 : 5 tags `v1.1.3 → v2.0.0` poussés sans bump, hotfix `v2.0.1` pour rattraper).

À chaque release du provider (`vX.Y.Z`), **mettre à jour systématiquement AVANT le tag** :

1. **`docs/index.md`** — typiquement 2-3 occurrences :
   ```hcl
   required_providers {
     ccp = {                                    # local name = ccp depuis v4.0.0
       source  = "cetic-group/ccp"
       version = "~> X.Y"   # ← bump à la version qu'on tag
     }
   }
   ```
   Local name = `ccp` (matche le snippet "Use Provider" du Registry ET le
   préfixe des ressources `ccp_*` → plus besoin de `provider = ...` par bloc).

2. **`README.md`** — section "Installation" + exemples + badges : aligner sur `~> X.Y` (compat majeure) ou `>= X.Y.Z` (lock à la min).

3. **`examples/**/main.tf`** — chaque répertoire `examples/` a sa contrainte. Bumper en masse :
   ```bash
   OLD="~> 2.0"; NEW="~> 2.1"
   grep -rln "version = \"$OLD\"" examples/ | xargs sed -i "s|version = \"$OLD\"|version = \"$NEW\"|g"
   ```

4. **`docs/resources/*.md`** et **`docs/data-sources/*.md`** — si une resource/datasource a été ajoutée ou modifiée dans cette release, sa doc doit refléter le schema final. Particulièrement vérifier :
   - Les attributs Required vs Optional vs Computed sont fidèles au code Go (`internal/resources/<r>/<r>.go::Schema()`)
   - Les exemples HCL utilisent les bons noms de champs (pas `label` quand le code dit `name`, etc.)
   - Les "Notes" mentionnent toute breaking change

5. **`internal/provider/version.go`** (si présent) — le provider expose sa version au Registry via ce constant.

6. **`.goreleaser.yml`** — pas de modif systématique mais vérifier que les binaires sont bien build pour les 6 plateformes (linux/macos/windows × amd64/arm64).

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
#    https://registry.terraform.io/providers/cetic-group/ccp/latest
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

### Live Registry

**Latest** : `v5.2.0` (2026-06-18) — feat : **version Kubernetes par node pool** — `ccp_k8s_node_pool.k8s_version` (**Optional+Computed**, NON ForceNew → mutable = rolling upgrade ; omis = hérite du control-plane ; readback null quand « hérite ») + `ccp_k8s_cluster.initial_pool.k8s_version` (Optional, pinne la version worker du pool initial au create, réconcilié in-place à l'update comme le reste du bloc). Contrainte serveur : worker `<=` control-plane (`k8s_version`), sinon 422. Client structs `K8sNodePool`/`K8sNodePoolCreateRequest`/`K8sNodePoolUpdateRequest`/`K8sInitialPool` gagnent `K8sVersion *string` (`k8s_version,omitempty`). Additif non-breaking. Exemples restent `~> 5.0` (5.2.0 ∈ ~> 5.0). Construit sur `os_image` (v5.1.0).

**Précédent** : `v5.1.0` (2026-06-18) — feat : **`ccp_k8s_cluster.os_image`** (famille OS des nœuds : `flatcar` défaut / `ubuntu` / `rocky9`, Optional+Computed+ForceNew, validator OneOf `flatcar`/`ubuntu`/`rocky9`) + datasource `ccp_k8s_templates.os` (slug, Computed). Backend #460 déjà live. Additif non-breaking. Détails dans « Historique récent ».

**Précédent** : `v4.9.0` (2026-06-13) — feat : **`ccp_bastion` parité avec `ccp_vpn_gateway`** — ajoute `plan` (small/medium/large, défaut `small`, ForceNew), `vpc_ids` (List, multi-VPC 1–5, ForceNew, le `vpc_id` primaire reste Required et toujours inclus), `public_ip_id` (Optional+Computed, ForceNew) et `tags` (List, ForceNew) + attribut Computed `public_ip_address`. Backend (#307, plateforme v2.16.x) acceptait déjà ces champs (`BastionCreate` merge `vpc_id`+`vpc_ids`, `BastionResponse` renvoie `plan`/`vpc_ids`/`public_ip_id`/`public_ip_address`/`tags`). **Additif, non-breaking** : `vpc_id` inchangé, tout le reste Optional. Exemples pinnés `~> 4.9`. Calqué EXACTEMENT sur `ccp_vpn_gateway` (schéma, applyToModel pitfall #5, helpers `listToStrings`/`optStr`).

**Historique récent** :
- `v5.2.0` — feat : version Kubernetes par node pool. `ccp_k8s_node_pool.k8s_version` (Optional+Computed, mutable/rolling — PAS ForceNew ; omis = hérite du control-plane ; readback null = « hérite », sinon la version effective renvoyée par l'API) wired Create+Update (envoie au PATCH seulement si set, change = rolling upgrade) + Read (mappe `*string` → null si nil). `ccp_k8s_cluster.initial_pool.k8s_version` (Optional non-Computed, comme le reste du bloc car `stateFromAPI` préserve `currentInitial` = pas de readback) wired au Create + réconcilié in-place à l'Update du cluster (PATCH du node pool initial). Contrainte backend : worker `<=` control-plane (422 sinon). 4 structs client (+`K8sVersion *string`). Construit sur v5.1.0 (`os_image` déjà fait, non touché). Additif, exemples `~> 5.0`.
- `v5.1.0` — feat : `ccp_k8s_cluster.os_image` (famille OS des nœuds : `flatcar` défaut / `ubuntu` / `rocky9`, Optional+Computed+ForceNew, validator OneOf) ; le datasource `ccp_k8s_templates` expose aussi `os` (slug, Computed). Backend (#460) déjà live : `POST /v1/k8s/clusters` accepte `os_image`, GET/list cluster le renvoient, items `GET /v1/k8s/templates` renvoient `os`. Additif non-breaking (configs existantes restent en `flatcar`). Create forward `os_image` uniquement si concret (sinon défaut serveur, relu en Computed) — même pattern que `tier`. Exemples restent `~> 5.0` (5.1.0 ∈ ~> 5.0).
- `v5.0.0` — **BREAKING** : drop `ccp_windows_instance` (legacy `/v1/windows-instances` supprimé côté plateforme ; Windows = VM QEMU native via `ccp_vm_instance` + `windows_license_consent`).
- `v4.9.0` — feat : `ccp_bastion` `plan` + `vpc_ids` + `public_ip_id` + `tags` + `public_ip_address` (parité vpn_gateway). Voir « Latest ».
- `v4.3.0 → v4.8.0` (résumé MEMORY, sessions interleavées, commits directs main) : `v4.3.0` `ccp_vpc.cidr` (Optional+Computed+ForceNew, bloc RFC1918) ; séquence k8s/CCKS (`initial_pool` labels/taints/min-max/autoscaler) déjà couverte par l'historique v3.x rebasée ; `v4.5.0`–`v4.7.0` cascade **VPN #306** (`ccp_vpn_gateway`/`ccp_vpn_peer`/`ccp_vpn_policy`) + **Bastion #307** (`ccp_bastion` v1 : `name`/`region`/`vpc_id`) ; `v4.8.0` (#343) stats accès/réseau (`bastion_access` write-only sur instances/scale-sets/templates). Détails dans les notes monorepo (MEMORY plateforme).
- `v4.1.4` — fix : Read AppGW (target groups / routes / members) via **list + filtre client-side** — l'API n'expose pas de GET individuel sur ces sous-ressources → 405 au premier refresh. Cf. piège #6. PR #49.
- `v4.1.3` — fix : preserve `target_group_id` quand la réponse API member l'omet (piège #5). PR #48.
- `v4.1.2` — fix : preserve `vpc_id` quand la réponse API AppGW l'omet (piège #5). PR #47.
- `v4.1.1` — fix : retrait de la validation client-side des plans AppGW (le catalogue serveur est la source de vérité — les clés réelles sont `appgw-small|medium|large`). PR #46.
- `v4.1.0` — feat+fix : **`ccp_public_ip.label` + `description`** (annotations mutables in-place) ; **rework `ccp_load_balancer`** sur le vrai contrat backend (listeners au POST initial, `listen_port`, algorithm `roundrobin|leastconn|source`, protocol `https`, **certificats Let's Encrypt ACME http01/dns01**, health checks — l'ancienne implémentation appelait des endpoints inexistants) ; **`ccp_appgw_listener` ACME** (`acme_challenge`/`acme_dns_provider`/`acme_dns_credentials`, retrait `custom_domain` no-op) ; nouveau datasource **`ccp_acme_dns_providers`**. Quantité d'IPs = `count` natif Terraform. PR #45. Cascade : modules v0.23.0, CLI v0.19.0, docs monorepo v2.8.4.
- `v4.0.1` — docs-only : retrait `endpoint`/`CCP_API_URL` des exemples (l'URL API est toujours la prod par défaut).
- `v4.0.0` (2026-06-01) — **adresse Registry renommée** `cetic-group/cetic-cloud-platform` → **`cetic-group/ccp`** + TypeName `ccp`. **Migration consommateur** : `source = "cetic-group/ccp"`, bloc `ccp = {}`, `terraform init -upgrade`. L'ancienne adresse n'est plus publiée. Module path Go = `github.com/cetic-group/terraform-provider-ccp`. Ressources `ccp_*` inchangées. Cascade : modules v0.22.0.
- `v3.2.1` — dernière version sous l'ancienne adresse `cetic-group/cetic-cloud-platform`.
- `v3.2.0` — feat : `ccp_k8s_cluster.initial_pool` expose `labels` (map) + `taints` (set `{key,value?,effect}`), parité avec `ccp_k8s_node_pool`. Le backend cluster-create acceptait déjà labels/taints sur l'initial pool (`K8sNodePoolCreate`), seul le client provider ne les envoyait pas. Mutables in-place : l'`Update` réconcilie le pool initial (replicas + min/max + labels + taints) via `UpdateK8sNodePool`. Optional (non-Computed) car `stateFromAPI` préserve le plan (`currentInitial`), pas de readback → pas de perma-diff. PR #43.
- `v3.1.2` — fix : `ccp_k8s_node_pool` — désactiver l'autoscaler (retirer `min_size`/`max_size`) ne lève plus « inconsistent result: was null, but now 0 ». `setState` normalise l'état désactivé renvoyé par l'API (0/0) vers null/null : comme `min_size`/`max_size` sont `Optional` (non-Computed), le state final doit == la config (null). L'autoscaler est désactivé ⟺ `max_size` absent ou 0 → on mappe alors **les deux** à null ; quand activé (`max_size > 0`), on garde les valeurs réelles, y compris `min_size=0` (scale-to-zero). Bug latent depuis v0.5.0, exposé en désactivant l'autoscaler d'un pool existant. PR #42.
- `v3.1.1` — fix : `ccp_k8s_cluster.initial_pool` — le retrait de `min_size`/`max_size` **désactive** l'autoscaler (envoie `0`/`0` au lieu de ne rien envoyer). Le PATCH backend ne peut pas effacer un `None` mais applique `0` → annotations autoscaler `min=0/max=0` = autoscaler off, pool figé à `replicas`. Toggle par présence cohérent avec `ccp_k8s_node_pool` et la console. Lève la limite documentée en v3.1.0 (« recréer le pool pour retirer »). PR #41.
- `v3.1.0` — feat : `ccp_k8s_cluster.initial_pool` expose `min_size`/`max_size` (parité avec `ccp_k8s_node_pool`) → autoscaler activable sur l'initial pool. **Mutable in-place** : le `Update` réconcilie l'initial pool (replicas + min/max) en retrouvant son node pool par nom et en le PATCHant (`ListK8sNodePools` + `UpdateK8sNodePool`) — corrige aussi le trou pré-existant où `initial_pool.replicas` n'était pas propagé. PR #40.
- `v3.0.0` — **BREAKING** : `ccp_k8s_cluster` — l'attribut `public_ip_id` est **supprimé** ; `apiserver_public_ip_id` devient l'**unique** levier de l'IP publique apiserver, désormais **mutable** (attach/détach/rotate sans ForceNew, à la création comme après coup), relu du backend (Optional+Computed). Avant : 2 attributs concurrents sur la même colonne backend (`apiserver_public_ip_id` create-only ForceNew + `public_ip_id` mutable) → confusion + IP non rattrapable si l'attach create ratait. Le provider attache désormais via `/attach-ip` après provisioning (create) et sur changement (update). Datasource : `public_ip_id` → `apiserver_public_ip_id`. **Migration consumer** : renommer `public_ip_id` → `apiserver_public_ip_id`. PR #39.
- `v2.0.4` — fix : `status` (Computed) de `ccp_k8s_node_pool` + `ccp_k8s_cluster` n'a plus `UseStateForUnknown()`. Ce plan modifier pinnait la valeur d'état précédente ("active") au plan d'un update, mais l'apply retourne l'état transitoire ("updating", reconcile async) → "Provider produced inconsistent result after apply". `status` est volatil → known-after-apply. Règle : `UseStateForUnknown` réservé aux Computed **immuables** (id, created_at), jamais un statut. PR #37.
- `v2.0.3` — fix : `ccp_k8s_cluster` Update n'envoie plus `ingress_public_ip_id`/`ingress_internal_ip` vides dans le PATCH. Ces champs Computed sont `known-after-apply` (Unknown) quand le scope ingress change → `.ValueString()` d'un Unknown = "" → `PATCH` avec `ingress_public_ip_id: ""` → backend 422 "valid UUID, found 0". Guard `IsNull/IsUnknown` + non-vide (aligné sur le Create). PR #35.
- `v2.0.2` — fix : `Delete` attend la suppression réelle (poll `GetX` jusqu'au 404) sur les ressources à teardown async qui ne waitaient pas → évite le `409 "existe déjà"` sur un replace (destroy-then-create même nom). Nouveau helper `client.PollUntilDeleted`. Couvre `ccp_k8s_cluster`, `ccp_load_balancer`, `ccp_application_gateway`, `ccp_registry`, `ccp_db_{pg,valkey,mysql,ferretdb}`. (container/vm/vpc/vnet/object_bucket/block_volume avaient déjà un poll-delete.) Aucun changement de schéma. PR #33.
- `v2.0.1` — docs catch-up : bump 7 fichiers exemples (`~> 1.1` / `~> 0.x` → `~> 2.0`). Aucun changement de schéma. PR #31.
- `v2.0.0` — **BREAKING** : drop `ccp_lxc_templates` + `ccp_qemu_templates` (deprecated en v1.2.0). Backend API inchangé. PR #30.
- `v1.2.0` — feat : nouveaux datasources canoniques `ccp_container_templates` + `ccp_vm_templates`. Anciens `ccp_lxc_templates`/`ccp_qemu_templates` marqués `DeprecationMessage`. PR #29.
- `v1.1.5` — docs : fix split sidebar Registry (Database/Databases, Network/Networking — 12 fichiers frontmatter). PR #28.
- `v1.1.4` — docs : DB ×4 + LB params manquants (`storage_gb`, `replicas`, `scale_set_id`, `cpu_millicores`, `memory_mb`, `endpoint_vnet_ip`). PR #27.
- `v1.1.3` — docs : full ingress controller coverage sur `ccp_k8s_cluster` (5 params ingress + 2 apiserver + tableau 4 combinaisons class × scope) + anti-leak (drop LXC/Keepalived/VRRP/HAProxy/Proxmox/VIP/BGP/DNAT/L2 announce/BPF/NodePort). PRs #25 + #26.

**Cascade modules après chaque release** : `cetic-cloud-terraform-modules` v0.18.0 = bump constraint `>= 2.0.0` sur les 46 fichiers `versions.tf` + `examples/` + `README` + `CHANGELOG`.

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

### Metadata.TypeName — hardcoded `ccp_*` (depuis v1.0.0)

Le provider's `Metadata.TypeName = "ccp"` (matche le snippet Registry ET le
préfixe). Les **resource/datasource types** restent en `ccp_*`. Le
découplage se fait en hardcodant chaque `Metadata` :

```go
func (r *vpcResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.TypeName = "ccp_vpc"   // ← HARDCODÉ, pas `req.ProviderTypeName + "_vpc"`
}
```

**Piège** : `req.ProviderTypeName` vaudrait `ccp` → ici ça coïnciderait, mais
on garde le hardcode `ccp_<r>` pour rester robuste à un futur rename du TypeName
(et parce que des resources ont un préfixe composite, ex `ccp_db_*`).
**Toute nouvelle resource/datasource doit suivre ce pattern.**

Sed safety check (post-rename) :
```bash
grep -rln 'req\.ProviderTypeName' internal/resources/ internal/datasources/
# DOIT retourner vide
```

Voir mémoire `feedback-tf-provider-typename-metadata-sed-regex`.

### Optional+Computed+UseStateForUnknown — pattern anti-perma-diff

Pour tout champ que l'API peut populer (mirror) et que le user peut aussi
set explicitement, schéma typique :

```go
"public_ip_id": schema.StringAttribute{
    Optional: true,
    Computed: true,                                  // ← obligatoire
    PlanModifiers: []planmodifier.String{
        stringplanmodifier.UseStateForUnknown(),     // ← obligatoire
    },
},
```

Sans ça → perma-diff dès que l'API mirror une valeur (ex. `ccp_public_ip.attached_to_id`
qui prend la valeur `vm_instance_id` après attach via une autre resource).
Voir mémoire `feedback-tf-optional-computed-use-state-for-unknown`.

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

### 5. La réponse API ne renvoie pas tous les champs du model → "inconsistent result after apply"

Cause typique : `applyToModel` mappe **inconditionnellement** tous les champs de la struct client vers le model
(`m.X = types.StringValue(api.X)`), mais le schéma de réponse de l'API (`*Response` Pydantic) **n'inclut pas ce champ**
→ Go le désérialise en zero value (`""`) → la valeur configurée est écrasée → Terraform refuse le state.

→ **Avant d'écrire un mapping, comparer champ par champ la struct Go client avec le schéma `*Response` de l'API**
(`apps/api/app/schemas/*.py` côté plateforme). Pour tout champ absent de la réponse : préserver la valeur
configurée/connue (`if api.X != "" { m.X = ... }`).

→ **Les fixtures de test doivent refléter le VRAI contrat de réponse** — une fixture qui simule un champ que l'API ne
renvoie pas masque exactement ce bug (c'est ce qui est arrivé).

Exemples historiques (saga apply kidshop, 2026-06-02) : `ccp_application_gateway.vpc_id` (fix v4.1.2),
`ccp_appgw_target_group_member.target_group_id` (fix v4.1.3).

### 6. L'API n'expose que des endpoints LIST → 405 sur les Read

Cause typique : le client provider suppose un `GET /<collection>/{id}` qui n'existe pas — l'API CCP n'expose souvent
que le LIST pour les sous-ressources (listeners, target groups, routes, members). Le 405 n'apparaît qu'au **premier
refresh** (Create/Delete marchent), donc pas détecté à l'apply initial.

→ **Vérifier les `@router.get(...)` réellement déclarés côté plateforme** avant d'écrire un client. Pour les
sous-ressources sans GET individuel : pattern **list + filtre client-side** (cf. `GetAppGWListener`, et depuis
v4.1.4 `GetAppGWTargetGroup`/`GetAppGWRoute`/`ListAppGWTargetGroupMembers`).

Exemple historique : fix v4.1.4 — `terraform plan` échouait en 405 sur toute infra AppGW existante.

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
go build -o ./terraform-provider-ccp .
```

Le binaire est ensuite picked up par les modules consommateurs si leur `~/.terraformrc` a un `dev_overrides` qui pointe vers ce dossier.
