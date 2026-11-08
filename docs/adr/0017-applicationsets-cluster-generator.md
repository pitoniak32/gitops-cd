# ApplicationSets with the cluster generator

Status: accepted. Replaces the bootstrap and add-on inlining parts of ADR 0015's wiring; the topology (hub + one Argo CD and Kargo shard per region) is unchanged.

Before, each Argo CD got hand-picked Applications from the host: `mise run infra-render` inlined every add-on's `values.yaml` and `manifests/` into an Application (`__SHARD__` sed for regional ones), `infra` applied them with `kubectl`, and two different root apps synced `rendered/kargo` (hub) and `rendered/argocd/<region>/` (regions). The renderer emitted one Application per app+target. Argo CD never read `platform/` from git.

Now every Argo CD follows Argo CD's [cluster bootstrapping](https://argo-cd.readthedocs.io/en/stable/operator-manual/cluster-bootstrapping/) pattern with ApplicationSets:

- **Cluster identity.** Each Argo CD labels its own `in-cluster` Secret through the argo-cd chart's `configs.clusterCredentials`: hub `example.com/role: hub`; region `example.com/role: region`, `example.com/region: <r>`.
- **One root app.** `platform/bootstrap/root.yaml` is identical everywhere and syncs `platform/appsets/` from `main`. Cluster generators decide what lands where; an AppSet whose selector matches nothing in an Argo CD generates nothing there.
- **Add-ons are git-sourced.** `platform/appsets/addons/<addon>.yaml`: chart pin + `$values` ref to `platform/infrastructure/<addon>/values.yaml`, or a kustomize path for `manifests/`. Region-specific values live in the template's `valuesObject` (`{{.values.region}}`, from the cluster label). Hub-side per-region add-ons (`kargo-ctrl-<r>`, `argocd-ui-<r>`) use a matrix of the hub cluster × git files `rendered/infrastructure/regions/*.json`: the hub has no region clusters to enumerate.
- **Workloads.** The renderer emits params, not Applications: `rendered/argocd/deploys/<r>/<cap>/<app>-<target>.json` (+ `rendered/argocd/projects/<r>/<cap>.yaml` AppProjects, synced by `argocd-projects`). `platform/appsets/workloads.yaml` is a matrix of this region's cluster × git files under its region. Application names, labels and the Kargo `authorized-stage` annotation are unchanged.
- **Region registry.** `platform/render/clusters.cue` is the single source. `mise run regions` regenerates the committed region-derived config (`platform/cluster/regions`, Keycloak `argocd-<r>` clients, Kargo `api.argocd.urls`); CI fails on drift. Keycloak imports a single realm file, so its clients can't come from `rendered/`.
- **Ordering.** AppSets create all add-ons at once. `infra` still waits in dependency order (CRDs, webhooks, issuer before users; argocd last); `SkipDryRunOnMissingResource` + sync retries absorb races. Every AppSet sets `preserveResourcesOnDeletion`, so deleting one never cascades to Argo CD itself or to workloads.

## Considered Options

- **Hub-and-spoke (hub Argo CD registers all regions).** The textbook cluster-generator use, one AppSet fans out. Rejected: the hub would hold region credentials, against ADR 0015 and Kargo's shard model.
- **Keep add-ons inlined, AppSet templates applied by `infra`.** No push needed for infra changes, but only replaces the `__SHARD__` sed; Argo CD still never sees git for `platform/`.
- **One AppSet per capability with list generator elements.** Bigger rendered YAML, and generator selectors can't compare a list element's region with the cluster's, so it would still need per-region AppSets.
- **Plain Applications for workloads, AppSets only for add-ons.** Least churn; doesn't make the workload side cluster-aware.

## Consequences

- Infrastructure changes (`platform/infrastructure/**`, `platform/appsets/**`) reach clusters only after they're pushed to `main`; `infra` only bootstraps and waits. Unpushed infra changes can't be tried with `infra`.
- After editing `clusters.cue` (new region): `mise run regions`, merge (CI renders `rendered/infrastructure/regions/`), clean `mise run up`.
- Migration is a clean `mise run up`; there is no in-place upgrade path from the inlined layout.
- The regional Argo CDs now run the ApplicationSet controller.
- Kargo's `argocd-update` only sets `operation` on AppSet-owned Applications; the AppSet controller doesn't own that field.
