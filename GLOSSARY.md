# gitops

Delivery configuration for all capabilities: owning teams describe what runs where in YAML; the platform validates it, renders Kubernetes manifests, and promotes them through each capability's pipeline.

## Language

### Capability config

**Capability**:
A business capability, one folder under `capabilities/`; its stages, targets and apps. The folder name is the identifier.
_Avoid_: team folder, tenant, domain

**Team**:
A group of people defined in `teams/<team>.yml` (GitHub team + identity-provider group). Teams own apps; they don't own folders.
_Avoid_: org, squad

**Owner**:
A team listed for an app (`apps/<app>/owners.yml`, else the capability's `owners.yml`). Owners approve the app's changes and promote it to any stage.
_Avoid_: maintainer, admin

**Stage**:
A named step in a capability's promotion graph (`stages/<name>.yml`), with the stages it depends on.
_Avoid_: environment, phase

**Target**:
A reusable deployment destination (`targets/<name>.yml`): one cluster + namespace + workload identity.
_Avoid_: env, region, location

**App**:
A deployable workload (`apps/<name>/app.yml`) assigned to targets per stage.
_Avoid_: service, component

**Layer**:
The overridable settings (`replicas`, `env`, `resources`) that can be set at app, stage, or target level; the most specific layer wins.
_Avoid_: overlay, values

### Platform

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
Copying one stage's rendered manifests from a Freight commit onto that stage's branch.
_Avoid_: deploy (as a verb for this step)

**Stage branch**:
The branch `env/<cap>/<app>/<stage>` that Argo CD syncs a stage's targets from.

### Validation

**Editor schema**:
The generic JSON Schema in `platform/schemas/` that editors use; it checks structure but not names that depend on other files.

**Lint**:
The full CUE validation over a capability's files (+ `teams/`) (`scripts/lint.sh`), run by CI and reported as `file:line:col` errors.
