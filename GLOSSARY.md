# gitops

Delivery configuration for all capabilities: owning teams describe what runs where in YAML; the platform validates it, renders Kubernetes manifests, and promotes them through each capability's pipeline.

## Language

### Capability config

**Capability**:
A business capability, one folder under `capabilities/`; its stages, targets and apps. The folder name is the identifier.
_Avoid_: team folder, tenant, domain

**Team**:
A GitHub team registered in `teams/<team>.yml`, used only for code review (CODEOWNERS). Teams own apps; they don't own folders. Teams grant no UI access.
_Avoid_: org, squad

**Owner**:
A team listed under `owners` for an app (`apps/<app>/owners.yml`, else the capability's `owners.yml`). Owners approve the app's code changes.
_Avoid_: maintainer, admin

**Promoter**:
An email listed under `promoters` for an app (same fallback as owners). Promoters promote the app to any stage, approve its Freight, and view the capability.
_Avoid_: deployer, owner (owners review code; promoters ship)

**Break-glass**:
An email listed under `breakGlass` in `owners.yml`. Only break-glass users (and admins) can approve Freight that skipped upstream verification (Kargo's "Manual approval required"), then promote it. Reserved for critical promotions (ADR 0013).
_Avoid_: approver (promoters approve normal promotions by promoting)

**Viewer**:
An email listed under `viewers` in `owners.yml`. Viewers see the whole capability in Kargo and Argo CD, read-only.

**Stage**:
A named step in a capability's promotion graph (`stages/<name>.yml`), with the stages it depends on. In Kargo it becomes a gate plus one target stage per target.
_Avoid_: environment, phase

**Gate**:
The Kargo Stage `<app>-<stage>` for a stage: control flow only (no promotion steps). It takes Freight once it's verified in every target of the upstream stage(s), and its targets take Freight from it.
_Avoid_: stage (ambiguous in Kargo)

**Target stage**:
The Kargo Stage `<app>-<target>` that promotes one target: it copies that target's rendered folder to its target branch and syncs its Argo CD app.

**Target**:
A reusable deployment destination (`targets/<name>/target.yml`): one cluster + namespace + workload identity.
_Avoid_: env, region, location

**Environment-variables file**:
A flat `KEY: value` file of a target (`targets/<t>/environment-variables/<name>.yml`, e.g. `launchdarkly.yml`). An app pulls it in with `envFrom: [<name>]` at app, stage or target level; every target it reaches must have that file. `env` in `app.yml` wins over it; between files, the most specific `envFrom` layer wins.
_Avoid_: env bundle, env file

**App**:
A deployable workload (`apps/<name>/app.yml`) assigned to targets per stage.
_Avoid_: service, component

**Layer**:
The overridable settings (`replicas`, `env`, `resources`) that can be set at app, stage, or target level; the most specific layer wins.
_Avoid_: overlay, values

### Platform

**Infrastructure**:
The cluster add-ons in `platform/infrastructure/<addon>/` (Argo CD, Traefik, cert-manager, Argo Rollouts, Keycloak, Kargo), managed by Argo CD and applied in order by `mise run infra`.
_Avoid_: platform (overloaded with the platform team)

**Bootstrap**:
The root Argo CD Applications in `platform/bootstrap/` that sync `rendered/argocd` and `rendered/kargo` from `main`.
_Avoid_: app of apps (for this specific file)

**Cluster**:
A Kubernetes cluster from the platform-owned registry, accepting a fixed set of environments.

**Environment**:
The policy class of a target (`dev`, `staging`, `perf`, `prod`) that determines rules such as minimum replicas.
_Avoid_: stage (a stage is a capability's pipeline step; an environment is platform policy)

**Deploy**:
The fully merged configuration of one app on one target.
_Avoid_: instance, release

**Pipeline**:
An app's promotion graph: the capability's stages it uses, with skipped stages bypassed.

**Rendered manifests**:
The generated output under `rendered/`; never edited by hand.
_Avoid_: build output, artifacts

### Delivery

**Freight**:
A `main` commit that changed an app's rendered manifests; the unit Kargo promotes.
_Avoid_: release, version

**Warehouse**:
The Kargo resource per app that watches `rendered/apps/<cap>/<app>/` and produces Freight.

**Promotion**:
Copying one target's rendered manifests from a Freight commit onto that target's branch (target stage), or passing Freight through a gate.
_Avoid_: deploy (as a verb for this step)

**Target branch**:
The branch `env/<cap>/<app>/<target>` that Argo CD syncs one target from (manifests at the root). It doesn't exist until the target's first promotion; until then its Argo CD app shows sync `Unknown`.
_Avoid_: stage branch (the pre-ADR 0012 layout `env/<cap>/<app>/<stage>`)

### Validation

**Editor schema**:
The generic JSON Schema in `platform/render/schemas/` that editors use; it checks structure but not names that depend on other files.

**Lint**:
The full CUE validation over a capability's files (+ `teams/`) (`mise run lint`), run by CI and reported as `file:line:col` errors.
