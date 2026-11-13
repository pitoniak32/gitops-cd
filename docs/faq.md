# FAQ

## Kargo topology

### One Kargo per cluster?

No. Typical: **one Kargo control plane per org/platform**, on a management (hub) cluster, promoting to many clusters.

- Kargo never deploys to clusters. It writes git (rendered manifests here, ADR 0002) and asks Argo CD to sync. Target clusters are only Argo CD Application destinations.
- Projects, Warehouses, Stages, Freight live in one place → a dev→staging→prod pipeline spanning clusters is one Project with one Freight lineage.
- Kargo-per-cluster is an anti-pattern: pipelines + Freight split across instances, cross-cluster promotion needs hand-offs.
- Separate Kargo instances only for hard isolation (separate orgs, compliance boundaries).

### Topologies

| Topology | Argo CD | Kargo | When |
|---|---|---|---|
| Hub-and-spoke | one, on hub, manages all clusters | one, next to it | default, simplest |
| Sharded | one per management cluster / region | central API + UI + default controller; one controller **shard** per Argo CD | Argo CD already distributed |
| Kargo per cluster | per cluster | per cluster | avoid |

### What does this repo run?

Sharded (ADR 0015), on kind: a hub cluster runs the Kargo control plane (API, UI, default controller; `platform/infrastructure/kargo/`, chart 1.12.5) plus an Argo CD for infrastructure only. Each region (`us-east4`, `dc1`) is its own kind cluster with its own Argo CD and a Kargo controller shard named after the region (placement option A below). The hub runs no workloads and holds no region credentials. Regions come from the cluster registry (`platform/cli/internal/system/config.go`); a new region = a new `Clusters` entry, `gcd regions`, clean `gcd up`.

## Integrating with existing Argo CD in each management cluster

Scenario: several management clusters, each already running its own Argo CD for its region/fleet. Don't install a full Kargo in each. Use **sharding**: one central Kargo control plane, one Kargo controller per Argo CD.

### How sharding works

- Central cluster: Kargo API, UI, webhooks, management controller, all Kargo resources (Projects, Stages, Freight, Promotions).
- Each Stage/Warehouse is assigned a shard: `spec.shard` (webhook keeps it in sync with label `kargo.akuity.io/shard`). Unassigned → default controller.
- A controller with `controller.shardName: <name>` reconciles only its shard's resources and talks only to its Argo CD.
- Promotions run on the shard controller: git commit/push to `rendered/` branches, then `argocd-update` against its Argo CD's Applications.

### Placement options for shard controllers

**A. Controller in each management cluster (recommended)**
- Kargo chart installed per management cluster with `api.enabled: false`, `controller.shardName: <mgmt-cluster>`, plus webhooks/other components off as needed.
- `kubeconfigSecrets.kargo` → kubeconfig for the central Kargo cluster (reads/writes Stages, Freight, Promotions there).
- Argo CD access is local (`controller.argocd.namespace: argocd`); `argocd.dataPlane.install: true` creates RBAC on Applications.
- Pros: Argo CD API never exposed cross-cluster; outbound-only from mgmt cluster to central. Cons: one more release per mgmt cluster.

**B. All controllers in central cluster**
- One controller release per shard in the central cluster, each with `controller.kubeconfigSecrets.argocd` → kubeconfig for its management cluster.
- `argocd.dataPlane.install: true` must be applied in the mgmt cluster instead (RBAC for Applications lives where Argo CD lives).
- Pros: all Kargo in one place. Cons: central cluster holds credentials into every mgmt cluster.

Central install: `api.argocd.urls` maps shard name → that Argo CD's URL (UI deep links), e.g. `"": https://argocd.localhost`, `"eu-mgmt": https://argocd.eu.example`.

### How this repo does it

- Cluster registry (`platform/cli/internal/system/config.go`): each cluster's `Region` is its Kargo shard and its Argo CD.
- Renderer (`kargo.go`): target Stages get `spec.shard` = their cluster's region; control-flow Stages and Warehouses stay unsharded (hub default controller). Applications carry `kargo.akuity.io/authorized-stage: <project>:<stage>`.
- Argo CD params and AppProjects are rendered per region (`rendered/argocd/{deploys,projects}/<region>/`); Kargo resources stay central (`rendered/kargo/`).
- Every Argo CD runs the same root app (`platform/bootstrap/root.yaml`); cluster-generator labels pick what each deploys (ADR 0017).
- Shard controllers run in their region (`platform/appsets/addons/kargo-shard.yaml`) with a kubeconfig for the hub (`gcd kargo-shards` hands over the token); Argo CD access stays local.
- RBAC: AppProject `viewer-<app>` roles exist in every regional Argo CD; each has its own Keycloak client `argocd-<region>` with the `email` claim (ADR 0011). Kargo `promote-<stage>`/`viewers` roles (per app project) stay central.
- Git creds: shards read `kargo-shared-resources` from the hub through that kubeconfig.

### Gotchas

- Shard controller without `controller.isDefault: true` ignores unsharded Stages; exactly one controller should be default.
- Stage health uses Argo CD Application status → shard must see the Applications it promotes to; a Stage on the wrong shard sits `Unknown`.
- Argo CD auto-sync (`selfHeal`) + Kargo `argocd-update` coexist fine; Kargo only bumps target revision and triggers a sync.
- Versions: shard controllers and central Kargo should run the same chart version.
