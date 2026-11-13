# gitops

Delivery configuration for all systems: owning teams describe what runs where in YAML; the platform validates it, renders Kubernetes manifests, and promotes them through each system's pipeline.

## Language

### System config

**System**:
A business system, one folder under `systems/`; its stages, targets and apps. The folder name is the identifier.
_Avoid_: team folder, tenant, domain

**Team**:
A GitHub team registered in `teams/<team>.yml`, used only for code review (CODEOWNERS). Teams own apps; they don't own folders. Teams grant no UI access.
_Avoid_: org, squad

**Owner**:
A team listed under `codeOwners` for an app (`apps.<app>.codeOwners`, else the system's `codeOwners` in `owners.yml`). Owners approve the app's code changes.
_Avoid_: maintainer, admin

**Promoter**:
An email listed under `promoters.<stage>` in `owners.yml` (per-app stage override under `apps.<app>.promoters`). Promoters promote the app to that stage only, approve its Freight, and view the system.
_Avoid_: deployer, owner (owners review code; promoters ship)

**Break-glass**:
An email listed under `breakGlass` in `owners.yml`. Only break-glass users (and admins) can approve Freight that skipped upstream verification (Kargo's "Manual approval required"), then promote it. Reserved for critical promotions (ADR 0013).
_Avoid_: approver (promoters approve normal promotions by promoting)

**Viewer**:
An email listed under `viewers` in `owners.yml`. Viewers see the system's apps in Kargo and Argo CD, read-only; `apps.<app>.viewers` replaces them for that app (ADR 0029).

**Stage**:
A named step in a system's promotion graph (`stages/<name>.yml`), with the stages it depends on. In Kargo it becomes one target stage per target, plus control-flow Stages where needed (ADR 0012).
_Avoid_: environment, phase

**Control-flow Stage**:
A Kargo Stage with no promotion steps that always auto-promotes, rendered only where it helps (ADR 0012): `<S>-done` waits for all targets of stage S and fans out to several downstream targets; `<D>` fans into stage D's targets or gives a multi-target stage a one-click "Promote to downstream".
_Avoid_: gate (the old gate before every stage is gone), join

**Target stage**:
The Kargo Stage `<target>`, in its app's Kargo project, that promotes one target: it copies that target's rendered folder to its target branch and syncs its Argo CD app.

**Kargo project**:
One per app, `kargo-<sys>-<app>` (ADR 0029): the app's Warehouse, Stages and access. Its Stage names carry no app prefix.
_Avoid_: system project (gone)

**Promotion task**:
A platform `ClusterPromotionTask` (`promote-argocd`, `promote-gha`) holding the steps every target stage runs; the target stage passes only vars (ADR 0027).
_Avoid_: promotion template (Kargo has none to share; each Stage keeps its own small `promotionTemplate`)

**Target**:
A reusable deployment destination (`targets/<name>/target.yml`). `platform: K8S` (default): one cluster + namespace + workload identity. `platform: TERRAFORM`: a **terraform target**, one GCP project + region + GCS state backend + workload identity, used only by terraform apps (ADR 0021). `platform: CLOUD_RUN`: a **Cloud Run target**, one GCP project + region + workload identity, for image apps, deployed by gha (ADR 0024). Named `<region>-<environmentType>-<k8s|tf|cr>[-<qualifier>]` (ADR 0025); lint checks it against the target's own fields.
_Avoid_: env, region, location

**Environment-variables file**:
A flat `KEY: value` file of a target (`targets/<t>/environment-variables/<name>.yml`, e.g. `launchdarkly.yml`). An app pulls it in with `envFrom: [<name>]` at app, stage or target level; every target it reaches must have that file. `env` in `app.yml` wins over it; between files, the most specific `envFrom` layer wins.
_Avoid_: env bundle, env file

**App**:
A deployable workload (`apps/<name>/app.yml`) assigned to targets per stage. A K8s app sets `image` (targets K8S and/or CLOUD_RUN); a **terraform app** sets `module` instead and deploys only to terraform targets, through a GitHub Actions workflow (ADR 0021).
_Avoid_: service, component

**Module**:
A terraform app's Terraform module, pushed as an OCI artifact; discovered and pinned (by digest) in Freight like an image (ADR 0021).
_Avoid_: stack, workspace, package

**Layer**:
The overridable settings (`replicas`, `env`, `resources`) that can be set at app, stage, or target level; the most specific layer wins.
_Avoid_: overlay, values

### Platform

**Infrastructure**:
The cluster add-ons (Argo CD, Traefik, cert-manager, Argo Rollouts, Keycloak, Kargo, shards): one ApplicationSet each in `platform/appsets/addons/`, config in `platform/infrastructure/<addon>/` read from `main`; `mise run infra` waits for them in order.
_Avoid_: platform (overloaded with the platform team)

**Bootstrap**:
The root Application `platform/bootstrap/root.yaml`, identical in every Argo CD (hub + regions), syncing the ApplicationSets in `platform/appsets/` from `main` (ADR 0017).
_Avoid_: app of apps (for this specific file)

**Cluster labels**:
Labels on each Argo CD's own `in-cluster` Secret that its ApplicationSets' cluster generators select on: `example.com/role: hub|region`, `example.com/region: <r>`.
_Avoid_: cluster roles (Kubernetes RBAC term)

**Cluster**:
A Kubernetes cluster from the platform-owned registry, accepting a fixed set of environment types. Each belongs to one region.

**Hub**:
The kind cluster `kargo-demo` running the Kargo control plane, Keycloak, Traefik and the platform Argo CD. It runs no workloads and holds no credentials to regions (ADR 0015).
_Avoid_: management cluster, control cluster

**Region**:
A workload cluster `region-<r>` (listed in `platform/cluster/regions`; `<r>` from the fixed GCP/datacenter lists in the cluster registry, `platform/cli/internal/system/config.go`, e.g. `us-east4`, `dc1`) with its own Argo CD and Kargo shard. A cluster's `region` decides which Argo CD and shard deploy its targets.
_Avoid_: zone, location (a target is a deploy destination; a region is the Argo CD + Kargo shard behind it)

**Shard**:
The Kargo controller in a region (`shardName` = region). It talks to that region's Argo CD and connects back to the hub Kargo. Target stages run on their cluster's shard; control-flow Stages and Warehouses run on the hub's default controller.
_Avoid_: agent (Kargo's docs use it loosely)

**Environment**:
The free-form name of where a target runs (`dev`, `staging`, `perf`, `prod`, `qa1`); label `example.com/environment`.
_Avoid_: stage (a stage is a system's pipeline step)

**Environment type**:
Required `environmentType` of a target: `dev | npr | prd`. Platform policy keys on it (minimum replicas, no autoPromote to `prd`); label `example.com/environment-type`.

**Deploy**:
The fully merged configuration of one app on one target.
_Avoid_: instance, release

**Deploy with**:
How promotion applies a deploy: `argocd` (sync its Argo CD Application, default) or `gha` (dispatch a GitHub Actions `workflow` and wait for the run). A layer field, so it can differ per stage or target (ADR 0020).
_Avoid_: deployer (a person who ships is a promoter), runner

**Pipeline**:
An app's promotion graph: the system's stages it uses, with skipped stages bypassed.

**Rendered manifests**:
The generated output under `rendered/`, on the CI-written branch `rendered` (ADR 0002); never edited by hand.
_Avoid_: build output, artifacts

### Delivery

**Freight**:
A `rendered`-branch commit with the app's rendered manifests (the config) plus one discovered image or module (SemVer tag, pinned by digest); the unit Kargo promotes. Promoters assemble it by hand in the Kargo UI (ADR 0016).
_Avoid_: release, version

**Warehouse**:
The Kargo resource per app that watches `rendered/apps/<sys>/<app>/` and the app's image repo. Freight creation is Manual: it only discovers commits and image tags.

**Promotion**:
Copying one target's rendered manifests from a Freight commit onto that target's branch and pinning the Freight's image in its `deployment.yaml` (target stage), or passing Freight through a control-flow Stage.
_Avoid_: deploy (as a verb for this step)

**Target branch**:
The branch `env/<sys>/<app>/<target>` that Argo CD syncs one target from (manifests at the root). It doesn't exist until the target's first promotion; until then its Argo CD app shows sync `Unknown`.
_Avoid_: stage branch (the pre-ADR 0012 layout `env/<sys>/<app>/<stage>`)

**Pre-deploy check**:
A platform-owned GitHub Actions workflow Kargo dispatches to the app's source repo and waits for before a target Stage pushes anything; configured per environment type in `platform/cli/internal/system/config.go` (`PreDeploy`) (initially `prd`), never by teams. Any result but success fails the Promotion (ADR 0028).
_Avoid_: verification (that runs after the deploy), gate

**Source repo**:
The GitHub repo of an app's code, `sourceRepo` in `app.yml` (required); where pre-deploy checks are dispatched.
_Avoid_: image repo (`image`), config repo (this repo)

### Validation

**Lint**:
The full validation (`gcd lint`, package `system`) over a system's files (+ `teams/`), run by CI and reported as `file:line:col` errors.
