# ApplicationSets with the cluster generator

Every Argo CD (hub and regions, ADR 0015) follows Argo CD's [cluster bootstrapping](https://argo-cd.readthedocs.io/en/stable/operator-manual/cluster-bootstrapping/) pattern. Each one runs the same root app, and cluster labels decide what it deploys.

- **Cluster identity**: each Argo CD labels its own `in-cluster` Secret (argo-cd chart `configs.clusterCredentials`). The hub gets `example.com/role: hub`. A region gets `example.com/role: region` and `example.com/region: <r>`.
- **One root app**: `platform/bootstrap/root.yaml`, identical everywhere, syncs `platform/appsets/` from `main`. An AppSet whose selector matches nothing in an Argo CD generates nothing there.
- **Add-ons are git-sourced**: `platform/appsets/addons/<addon>.yaml` pins the chart and adds a `$values` ref to `platform/infrastructure/<addon>/values.yaml`, or a kustomize path for `manifests/`. Region values go in the template's `valuesObject` (`{{.values.region}}`). Hub-side per-region add-ons (`kargo-ctrl-<r>`, `argocd-ui-<r>`) use a matrix of the hub cluster × `rendered/infrastructure/regions/*.json`, because the hub has no region clusters to enumerate.
- **Workloads**: the renderer emits params, not Applications: `rendered/argocd/deploys/<r>/<sys>/<app>-<target>.json`, plus AppProjects `rendered/argocd/projects/<r>/<sys>.yaml`. `workloads.yaml` is a matrix of this region's cluster × the git files under its region.
- **Region registry**: `system/config.go` is the single source. `gcd regions` regenerates the committed region-derived config (`platform/cluster/regions`, Keycloak `argocd-<r>` clients, Kargo `api.argocd.urls`), and CI fails on drift. Keycloak imports one realm file, so its clients can't come from `rendered/`.
- **Ordering**: AppSets create all add-ons at once. `infra` waits only where a host step needs an app (`traefik` → `certs`, `kargo` → `creds`, shard apps → `kargo-shards`) and checks the rest. `SkipDryRunOnMissingResource` and sync retries absorb races. Every AppSet sets `preserveResourcesOnDeletion`.

## Considered Options

- **Hub-and-spoke** (the hub Argo CD registers all regions): the hub would hold region credentials (ADR 0015).
- **Add-ons inlined into Applications and applied by `infra`**: no push needed, but Argo CD never sees `platform/` in git, and region values need text substitution.
- **One AppSet per system with list generator elements**: generator selectors can't compare an element's region with the cluster's.

## Consequences

- Infrastructure changes reach clusters only after a push to `main`. `infra` only bootstraps and waits (`docs/sync-boundaries.md`).
- Deleting an AppSet or Application never cascades to Argo CD itself or to workloads. A removed workload must be cleaned up by hand.
- Kargo's `argocd-update` only sets `operation` on AppSet-owned Applications, and the AppSet controller doesn't own that field.
